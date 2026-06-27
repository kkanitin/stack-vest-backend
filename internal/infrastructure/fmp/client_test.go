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

	"github.com/kanitin/stackvest/backend/internal/domain/stock"
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

// TestGetDividendsCalendar_Parsing pins the mapping from FMP's documented
// /stable/dividends-calendar field names onto dividend.Event, and asserts the
// from/to range params are sent. A field-name drift would otherwise surface only as
// a silently empty dividend calendar (zero-valued fields), with all mock-based
// use-case tests still green. The raw JSON is deliberately a literal, not a
// struct-encoded fixture, so the test fails if the json tags stop matching FMP.
func TestGetDividendsCalendar_Parsing(t *testing.T) {
	// Sample shape taken from a live /stable/dividends-calendar response.
	const body = `[
		{"symbol":"EMYB","date":"2026-06-26","recordDate":"2026-06-26","paymentDate":"2026-07-14","declarationDate":"2026-06-17","adjDividend":0.55,"dividend":0.55,"yield":2.53,"frequency":"Annual"},
		{"symbol":"FGBI","date":"2026-06-26","recordDate":"2026-06-26","paymentDate":"2026-06-30","declarationDate":"2026-05-21","adjDividend":0.01,"dividend":0.01,"yield":0.37,"frequency":"Quarterly"}
	]`
	var gotFrom, gotTo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFrom = r.URL.Query().Get("from")
		gotTo = r.URL.Query().Get("to")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	events, err := client.GetDividendsCalendar(mustParseDate("2026-06-27"), mustParseDate("2026-09-25"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotFrom != "2026-06-27" || gotTo != "2026-09-25" {
		t.Errorf("range params: want from=2026-06-27 to=2026-09-25, got from=%s to=%s", gotFrom, gotTo)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	e := events[0]
	if e.Symbol != "EMYB" {
		t.Errorf("symbol: want EMYB, got %q", e.Symbol)
	}
	if !e.ExDate.Equal(mustParseDate("2026-06-26")) {
		t.Errorf("exDate (from \"date\"): want 2026-06-26, got %v", e.ExDate)
	}
	if !e.PaymentDate.Equal(mustParseDate("2026-07-14")) {
		t.Errorf("paymentDate: want 2026-07-14, got %v", e.PaymentDate)
	}
	if !e.DeclarationDate.Equal(mustParseDate("2026-06-17")) {
		t.Errorf("declarationDate: want 2026-06-17, got %v", e.DeclarationDate)
	}
	if e.Dividend != 0.55 {
		t.Errorf("dividend: want 0.55, got %v", e.Dividend)
	}
	if e.Frequency != "Annual" {
		t.Errorf("frequency: want Annual, got %q", e.Frequency)
	}
}

// TestGetDividendsCalendar_EmptyIsNotError ensures an empty window yields an empty
// slice rather than an error (so the use case negative-caches it).
func TestGetDividendsCalendar_EmptyIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	events, err := client.GetDividendsCalendar(mustParseDate("2026-06-27"), mustParseDate("2026-09-25"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
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

func TestSearchNameParsing(t *testing.T) {
	fixture := []fmpSearchResult{
		{Symbol: "GOOGL", Name: "Alphabet Inc.", Currency: "USD", ExchangeFullName: "NASDAQ Global Select", Exchange: "NASDAQ"},
	}

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	matches, err := client.SearchName("google")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/search-name") {
		t.Errorf("expected path ending in /search-name, got %s", gotPath)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Symbol != "GOOGL" {
		t.Errorf("expected symbol GOOGL, got %s", matches[0].Symbol)
	}
	if matches[0].Name != "Alphabet Inc." {
		t.Errorf("expected name Alphabet Inc., got %s", matches[0].Name)
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

func TestListStockSymbols(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"symbol":"AAPL","companyName":"Apple Inc."},{"symbol":"GOOGL","companyName":"Alphabet Inc."},{"symbol":""}]`))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	symbols, err := client.ListStockSymbols()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/stock-list") {
		t.Errorf("expected path ending in /stock-list, got %s", gotPath)
	}
	// Blank symbols are skipped.
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %v", len(symbols), symbols)
	}
	if symbols[0] != "AAPL" || symbols[1] != "GOOGL" {
		t.Errorf("unexpected symbols: %v", symbols)
	}
}

func TestListETFSymbols(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"symbol":"SPY","name":"SPDR S&P 500 ETF Trust"}]`))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	symbols, err := client.ListETFSymbols()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/etfs-list") {
		t.Errorf("expected path ending in /etfs-list, got %s", gotPath)
	}
	if len(symbols) != 1 || symbols[0] != "SPY" {
		t.Errorf("unexpected symbols: %v", symbols)
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

func TestGetProfile(t *testing.T) {
	// Raw JSON literal mirroring the real FMP /stable/profile response so the
	// struct tags are actually exercised against FMP's wire field names/types.
	const body = `[{
		"symbol": "AAPL",
		"price": 232.8,
		"marketCap": 3500823120000,
		"beta": 1.24,
		"companyName": "Apple Inc.",
		"currency": "USD",
		"exchange": "NASDAQ",
		"exchangeFullName": "NASDAQ Global Select",
		"industry": "Consumer Electronics",
		"website": "https://www.apple.com",
		"description": "Apple Inc. designs, manufactures and markets smartphones.",
		"ceo": "Mr. Timothy D. Cook",
		"sector": "Technology",
		"country": "US",
		"fullTimeEmployees": "164000",
		"image": "https://images.financialmodelingprep.com/symbol/AAPL.png",
		"ipoDate": "1980-12-12",
		"isEtf": false,
		"isActivelyTrading": true
	}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	profile, err := client.GetProfile("AAPL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Symbol != "AAPL" {
		t.Errorf("expected symbol AAPL, got %s", profile.Symbol)
	}
	if profile.CompanyName != "Apple Inc." {
		t.Errorf("expected company name Apple Inc., got %s", profile.CompanyName)
	}
	if profile.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", profile.Currency)
	}
	if profile.Exchange != "NASDAQ" || profile.ExchangeFullName != "NASDAQ Global Select" {
		t.Errorf("exchange fields not mapped: %q / %q", profile.Exchange, profile.ExchangeFullName)
	}
	if profile.Industry != "Consumer Electronics" || profile.Sector != "Technology" {
		t.Errorf("industry/sector not mapped: %q / %q", profile.Industry, profile.Sector)
	}
	if profile.CEO != "Mr. Timothy D. Cook" {
		t.Errorf("expected CEO Mr. Timothy D. Cook, got %s", profile.CEO)
	}
	if profile.Country != "US" {
		t.Errorf("expected country US, got %s", profile.Country)
	}
	if profile.FullTimeEmployees != "164000" {
		t.Errorf("expected fullTimeEmployees 164000, got %s", profile.FullTimeEmployees)
	}
	if profile.Price != 232.8 || profile.MarketCap != 3500823120000 || profile.Beta != 1.24 {
		t.Errorf("numeric fields not mapped: price=%v marketCap=%v beta=%v", profile.Price, profile.MarketCap, profile.Beta)
	}
	if profile.IPODate != "1980-12-12" {
		t.Errorf("expected ipoDate 1980-12-12, got %s", profile.IPODate)
	}
	if profile.IsEtf != false || profile.IsActivelyTrading != true {
		t.Errorf("bool flags not mapped: isEtf=%v isActivelyTrading=%v", profile.IsEtf, profile.IsActivelyTrading)
	}
}

func TestGetProfile_EmptyResultReturnsSymbolNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	_, err := client.GetProfile("ZZZZ")
	if !errors.Is(err, stock.ErrSymbolNotFound) {
		t.Fatalf("expected ErrSymbolNotFound, got %v", err)
	}
}

func TestGetBiggestGainers(t *testing.T) {
	fixture := []fmpMarketMover{
		{Symbol: "AAPL", ChangesPercentage: 5.5},
		{Symbol: "TSLA", ChangesPercentage: 3.2},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/biggest-gainers") {
			t.Errorf("expected path ending in /biggest-gainers, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	movers, err := client.GetBiggestGainers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(movers) != 2 {
		t.Fatalf("expected 2 movers, got %d", len(movers))
	}
	if movers[0].Symbol != "AAPL" || movers[0].ChangePercent != 5.5 {
		t.Errorf("unexpected first mover: %+v", movers[0])
	}
}

func TestGetBiggestLosers(t *testing.T) {
	fixture := []fmpMarketMover{
		{Symbol: "META", ChangesPercentage: -4.1},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/biggest-losers") {
			t.Errorf("expected path ending in /biggest-losers, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(fixture)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	movers, err := client.GetBiggestLosers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(movers) != 1 {
		t.Fatalf("expected 1 mover, got %d", len(movers))
	}
	if movers[0].Symbol != "META" || movers[0].ChangePercent != -4.1 {
		t.Errorf("unexpected mover: %+v", movers[0])
	}
}

func TestGetMarketMovers_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	movers, err := client.GetBiggestGainers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(movers) != 0 {
		t.Fatalf("expected empty slice, got %d movers", len(movers))
	}
}

func TestGetMarketMovers_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	if _, err := client.GetBiggestGainers(); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestGetMarketMovers_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}
	if _, err := client.GetBiggestGainers(); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}
