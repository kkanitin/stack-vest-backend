package fmp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

func TestListConstituents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dowjones-constituent" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`[{"symbol":"AAPL","name":"Apple Inc.","sector":"Technology","subSector":"Consumer Electronics"},{"symbol":""}]`))
	}))
	defer srv.Close()
	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	got, err := client.ListConstituents(market.IndexDow30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := market.Constituent{Symbol: "AAPL", Name: "Apple Inc.", Sector: "Technology", SubSector: "Consumer Electronics"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
}

func TestListConstituentsPlanRestricted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"Error Message":"Special Endpoint"}`))
	}))
	defer srv.Close()
	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	if _, err := client.ListConstituents(market.IndexSP500); !errors.Is(err, market.ErrPlanRestricted) {
		t.Fatalf("expected ErrPlanRestricted, got %v", err)
	}
}

func TestGetBatchQuotesChunks(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch-quote" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		calls.Add(1)
		syms := strings.Split(r.URL.Query().Get("symbols"), ",")
		parts := make([]string, len(syms))
		for i, s := range syms {
			parts[i] = `{"symbol":"` + s + `","price":10,"changePercentage":1.5,"marketCap":1000}`
		}
		w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	}))
	defer srv.Close()
	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	symbols := make([]string, batchQuoteChunk+5)
	for i := range symbols {
		symbols[i] = "S" + strings.Repeat("X", i%3) + string(rune('A'+i%26))
	}
	got, err := client.GetBatchQuotes(symbols)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 chunked calls, got %d", calls.Load())
	}
	if len(got) != len(symbols) {
		t.Fatalf("expected %d quotes, got %d", len(symbols), len(got))
	}
	if got[0].ChangePercent != 1.5 || got[0].MarketCap != 1000 {
		t.Errorf("unexpected quote %+v", got[0])
	}
}

func TestGetBatchQuotesFallsBackWhenRestricted(t *testing.T) {
	var batchCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/batch-quote":
			batchCalls.Add(1)
			w.WriteHeader(http.StatusPaymentRequired)
		case "/quote":
			sym := r.URL.Query().Get("symbol")
			if sym == "GONE" {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"symbol":"` + sym + `","price":5,"changesPercentage":-2,"marketCap":50}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := &Client{apiKey: "test", httpClient: srv.Client(), baseURL: srv.URL}

	for range 2 {
		got, err := client.GetBatchQuotes([]string{"AAA", "GONE", "BBB"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0].Symbol != "AAA" || got[1].Symbol != "BBB" || got[0].ChangePercent != -2 {
			t.Fatalf("unexpected quotes %+v", got)
		}
	}
	if batchCalls.Load() != 1 {
		t.Errorf("expected /batch-quote to be tried once, got %d", batchCalls.Load())
	}
}
