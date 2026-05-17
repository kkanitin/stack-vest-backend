package fmp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
