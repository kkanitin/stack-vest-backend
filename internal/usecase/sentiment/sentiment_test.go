package sentimentuc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/stock"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
)

type mockMarketDataProvider struct {
	quotes  map[string]*stock.Quote
	gainers []fmp.MarketMover
	losers  []fmp.MarketMover
	err     error

	quoteCalls  int
	gainerCalls int
	loserCalls  int
}

func (m *mockMarketDataProvider) GetQuote(symbol string) (*stock.Quote, error) {
	m.quoteCalls++
	if m.err != nil {
		return nil, m.err
	}
	return m.quotes[symbol], nil
}

func (m *mockMarketDataProvider) GetBiggestGainers() ([]fmp.MarketMover, error) {
	m.gainerCalls++
	if m.err != nil {
		return nil, m.err
	}
	return m.gainers, nil
}

func (m *mockMarketDataProvider) GetBiggestLosers() ([]fmp.MarketMover, error) {
	m.loserCalls++
	if m.err != nil {
		return nil, m.err
	}
	return m.losers, nil
}

func newMock() *mockMarketDataProvider {
	return &mockMarketDataProvider{
		quotes: map[string]*stock.Quote{
			vixSymbol:   {Symbol: vixSymbol, Price: 25},
			indexSymbol: {Symbol: indexSymbol, Price: 5000, ChangePercent: 1.5},
		},
		gainers: []fmp.MarketMover{{Symbol: "AAPL", ChangePercent: 5}, {Symbol: "TSLA", ChangePercent: 3}},
		losers:  []fmp.MarketMover{{Symbol: "META", ChangePercent: -2}},
	}
}

func TestExecute_ComputesCompositeAndCaches(t *testing.T) {
	mock := newMock()
	uc := NewUseCase(mock, time.Hour)

	result, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Signals.VIX != 25 || result.Signals.IndexChangePercent != 1.5 {
		t.Errorf("unexpected signals: %+v", result.Signals)
	}
	if result.Signals.GainersCount != 2 || result.Signals.LosersCount != 1 {
		t.Errorf("unexpected mover counts: %+v", result.Signals)
	}
	if result.Score < 0 || result.Score > 100 {
		t.Errorf("score out of range: %d", result.Score)
	}
	if result.Status == "" {
		t.Error("expected non-empty status")
	}
	if mock.quoteCalls != 2 || mock.gainerCalls != 1 || mock.loserCalls != 1 {
		t.Errorf("expected 2 quote, 1 gainer, 1 loser call; got %d, %d, %d", mock.quoteCalls, mock.gainerCalls, mock.loserCalls)
	}
}

func TestExecute_CacheHitSkipsFetch(t *testing.T) {
	mock := newMock()
	uc := NewUseCase(mock, time.Hour)

	first, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.Timestamp != second.Timestamp {
		t.Error("expected cached result to be returned (identical timestamp)")
	}
	if mock.quoteCalls != 2 || mock.gainerCalls != 1 || mock.loserCalls != 1 {
		t.Errorf("expected fetch only once; got %d quote, %d gainer, %d loser calls", mock.quoteCalls, mock.gainerCalls, mock.loserCalls)
	}
}

func TestExecute_PropagatesFMPError(t *testing.T) {
	mock := newMock()
	mock.err = errors.New("fmp unavailable")
	uc := NewUseCase(mock, time.Hour)

	if _, err := uc.Execute(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}
