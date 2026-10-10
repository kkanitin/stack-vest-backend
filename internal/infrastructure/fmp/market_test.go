package fmp

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
