package fmp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	apiKey := "test-api-key"
	client := NewClient(apiKey)

	if client.apiKey != apiKey {
		t.Errorf("expected apiKey %s, got %s", apiKey, client.apiKey)
	}

	if client.baseURL != "https://financialmodelingprep.com/stable" {
		t.Errorf("unexpected baseURL: %s", client.baseURL)
	}
}

func TestDecodeHistorical(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantLen      int
		wantAdjClose float64
	}{
		{
			"array",
			`[{"date":"2024-01-02","adjClose":185.5,"close":185.5},{"date":"2024-01-03","adjClose":184.0,"close":184.0}]`,
			2, 185.5,
		},
		{
			"wrapped object",
			`{"symbol":"AAPL","historical":[{"date":"2024-01-02","adjClose":185.5,"close":185.5}]}`,
			1, 185.5,
		},
		{"empty array", `[]`, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			points, err := decodeHistorical(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(points) != tc.wantLen {
				t.Fatalf("expected %d points, got %d", tc.wantLen, len(points))
			}
			if tc.wantLen > 0 && points[0].AdjClose != tc.wantAdjClose {
				t.Errorf("expected adjClose %f, got %f", tc.wantAdjClose, points[0].AdjClose)
			}
		})
	}
}

func TestGetHistoricalPrices_ArrayResponse(t *testing.T) {
	fixture := []fmpHistoricalPoint{
		{Date: "2024-01-03", AdjClose: 184.0, Close: 184.0},
		{Date: "2024-01-02", AdjClose: 185.5, Close: 185.5},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	prices, err := client.GetHistoricalPrices("AAPL", mustParseDate("2024-01-01"), mustParseDate("2024-01-05"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices, got %d", len(prices))
	}
	if prices[0].AdjClose != 185.5 {
		t.Errorf("expected first price 185.5 (ascending), got %f", prices[0].AdjClose)
	}
}

func TestGetHistoricalPrices_WrappedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"symbol":"AAPL","historical":[{"date":"2024-01-02","adjClose":185.5,"close":185.5}]}`))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	prices, err := client.GetHistoricalPrices("AAPL", mustParseDate("2024-01-01"), mustParseDate("2024-01-05"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("expected 1 price, got %d", len(prices))
	}
}

func mustParseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestSearchSymbolParsing(t *testing.T) {
	fixture := []fmpSearchResult{
		{Symbol: "AAPL", Name: "Apple Inc.", Currency: "USD", ExchangeFullName: "NASDAQ Global Select", Exchange: "NASDAQ"},
		{Symbol: "AAPL.TRT", Name: "Apple CDR (CAD Hedged)", Currency: "CAD", ExchangeFullName: "Toronto", Exchange: "TSX"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	matches, err := client.SearchSymbol("AAPL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Symbol != "AAPL" {
		t.Errorf("expected symbol AAPL, got %s", matches[0].Symbol)
	}
	if matches[0].Name != "Apple Inc." {
		t.Errorf("expected name Apple Inc., got %s", matches[0].Name)
	}
	if matches[0].Type != "NASDAQ" {
		t.Errorf("expected type NASDAQ, got %s", matches[0].Type)
	}
	if matches[0].Region != "NASDAQ Global Select" {
		t.Errorf("expected region NASDAQ Global Select, got %s", matches[0].Region)
	}
	if matches[0].Currency != "USD" {
		t.Errorf("expected currency USD, got %s", matches[0].Currency)
	}
}

func TestDoGet_RetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"symbol":"AAPL","name":"Apple Inc.","currency":"USD","exchangeFullName":"NASDAQ","exchange":"NASDAQ"}]`)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	matches, err := client.SearchSymbol("AAPL")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDoGet_ExhaustsRetriesReturnsErrRateLimited(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	_, err := client.SearchSymbol("AAPL")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDoGet_RespectsRetryAfterHeader(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"symbol":"AAPL","name":"Apple","currency":"USD","exchangeFullName":"NASDAQ","exchange":"NASDAQ"}]`)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	_, err := client.SearchSymbol("AAPL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestGetHistoricalPrices_FallsBackToCloseWhenAdjCloseMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"date":"2024-01-03","close":184.0},{"date":"2024-01-02","close":185.5}]`))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	prices, err := client.GetHistoricalPrices("AAPL", mustParseDate("2024-01-01"), mustParseDate("2024-01-05"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("expected 2 prices, got %d", len(prices))
	}
	if prices[0].AdjClose != 185.5 {
		t.Errorf("expected first price 185.5 (close fallback), got %f", prices[0].AdjClose)
	}
	if prices[1].AdjClose != 184.0 {
		t.Errorf("expected second price 184.0 (close fallback), got %f", prices[1].AdjClose)
	}
}

func TestSearchSymbolEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	matches, err := client.SearchSymbol("FAKE999XYZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}
