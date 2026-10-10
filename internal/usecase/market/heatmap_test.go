package marketuc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/market"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type fakeLister struct {
	byIndex map[domain.Index][]domain.Constituent
	err     error
}

func (f *fakeLister) ListConstituents(index domain.Index) ([]domain.Constituent, error) {
	return f.byIndex[index], f.err
}

type fakeQuoter struct{ quotes map[string]domain.Quote }

func (f *fakeQuoter) GetBatchQuotes(symbols []string) ([]domain.Quote, error) {
	var out []domain.Quote
	for _, s := range symbols {
		if q, ok := f.quotes[s]; ok {
			out = append(out, q)
		}
	}
	return out, nil
}

type fakeChanger struct{ failing map[string]bool }

func (f *fakeChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	if f.failing[symbol] {
		return nil, errors.New("boom")
	}
	return &stockdomain.PriceChange{Symbol: symbol, D5: 2, M1: 3, YTD: 4}, nil
}

func newTestUC(lister *fakeLister) *HeatmapUseCase {
	quoter := &fakeQuoter{quotes: map[string]domain.Quote{
		"AAPL": {Symbol: "AAPL", Price: 200, ChangePercent: 1, MarketCap: 3000},
		"MSFT": {Symbol: "MSFT", Price: 400, ChangePercent: -1, MarketCap: 3500},
		"JPM":  {Symbol: "JPM", Price: 150, ChangePercent: 0.5, MarketCap: 500},
		"ZERO": {Symbol: "ZERO", Price: 1, MarketCap: 0},
		"ODD":  {Symbol: "ODD", Price: 1, MarketCap: 10},
	}}
	return NewHeatmapUseCase(lister, quoter, &fakeChanger{failing: map[string]bool{"JPM": true}})
}

func TestGetBeforeRefreshIsNotReady(t *testing.T) {
	uc := newTestUC(&fakeLister{})
	if _, err := uc.Get(domain.IndexSP500); !errors.Is(err, domain.ErrHeatmapNotReady) {
		t.Fatalf("expected ErrHeatmapNotReady, got %v", err)
	}
}

func TestRefreshGroupsAndSorts(t *testing.T) {
	uc := newTestUC(&fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexSP500: {
			{Symbol: "JPM", Name: "JPMorgan", Sector: "Financials"},
			{Symbol: "AAPL", Name: "Apple", Sector: "Technology"},
			{Symbol: "MSFT", Name: "Microsoft", Sector: "Technology"},
			{Symbol: "ZERO", Name: "No cap", Sector: "Technology"},
			{Symbol: "NOQUOTE", Name: "Missing", Sector: "Technology"},
			{Symbol: "ODD", Name: "No sector"},
		},
	}})
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hm, err := uc.Get(domain.IndexSP500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hm.Sectors) != 3 {
		t.Fatalf("expected 3 sectors, got %+v", hm.Sectors)
	}
	tech := hm.Sectors[0]
	if tech.Name != "Technology" || tech.MarketCap != 6500 || len(tech.Stocks) != 2 {
		t.Fatalf("unexpected first sector %+v", tech)
	}
	if tech.Stocks[0].Symbol != "MSFT" || tech.Stocks[1].Symbol != "AAPL" {
		t.Errorf("stocks not sorted by market cap: %+v", tech.Stocks)
	}
	if hm.Sectors[1].Name != "Financials" || hm.Sectors[2].Name != otherSector {
		t.Errorf("sectors not sorted by market cap: %s, %s", hm.Sectors[1].Name, hm.Sectors[2].Name)
	}

	msft := tech.Stocks[0].Change
	if *msft.D1 != -1 || *msft.W1 != 2 || *msft.M1 != 3 || *msft.YTD != 4 {
		t.Errorf("unexpected MSFT change %+v", msft)
	}
	jpm := hm.Sectors[1].Stocks[0].Change
	if jpm.D1 == nil || *jpm.D1 != 0.5 || jpm.W1 != nil || jpm.YTD != nil {
		t.Errorf("failed price change should leave only 1D: %+v", jpm)
	}
}

func TestRefreshKeepsPreviousSnapshotOnFailure(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	uc := newTestUC(lister)
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first, _ := uc.Get(domain.IndexDow30)

	lister.err = errors.New("upstream down")
	if err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := uc.Get(domain.IndexDow30); got != first {
		t.Error("previous snapshot should be kept after a failed refresh")
	}
}

func TestRefreshStopsWhenCancelled(t *testing.T) {
	uc := newTestUC(&fakeLister{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := uc.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

type fakeStore struct {
	saved   map[domain.Index]*domain.Heatmap
	saveErr error
	loadErr error
}

func newFakeStore() *fakeStore { return &fakeStore{saved: map[domain.Index]*domain.Heatmap{}} }

func (f *fakeStore) Load(_ context.Context, index domain.Index) (*domain.Heatmap, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.saved[index], nil
}

func (f *fakeStore) Save(_ context.Context, hm *domain.Heatmap) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[hm.Index] = hm
	return nil
}

func TestRestoreServesStoredSnapshots(t *testing.T) {
	store := newFakeStore()
	stored := &domain.Heatmap{Index: domain.IndexSP500}
	store.saved[domain.IndexSP500] = stored
	uc := newTestUC(&fakeLister{}).WithStore(store)

	n, err := uc.Restore(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("expected 1 restored and no error, got %d, %v", n, err)
	}
	if got, err := uc.Get(domain.IndexSP500); err != nil || got != stored {
		t.Fatalf("expected the stored snapshot, got %v, %v", got, err)
	}
	if _, err := uc.Get(domain.IndexDow30); !errors.Is(err, domain.ErrHeatmapNotReady) {
		t.Errorf("index with nothing stored should stay not ready, got %v", err)
	}
}

func TestRestoreKeepsSnapshotsBuiltInMemory(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	store := newFakeStore()
	uc := newTestUC(lister).WithStore(store)
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	built, _ := uc.Get(domain.IndexDow30)

	store.saved[domain.IndexDow30] = &domain.Heatmap{Index: domain.IndexDow30}
	if n, _ := uc.Restore(context.Background()); n != 0 {
		t.Errorf("expected nothing restored over an in-memory snapshot, got %d", n)
	}
	if got, _ := uc.Get(domain.IndexDow30); got != built {
		t.Error("Restore replaced a snapshot built in memory")
	}
}

func TestRestoreReportsLoadErrors(t *testing.T) {
	store := newFakeStore()
	store.loadErr = errors.New("redis down")
	uc := newTestUC(&fakeLister{}).WithStore(store)
	if n, err := uc.Restore(context.Background()); err == nil || n != 0 {
		t.Fatalf("expected an error and nothing restored, got %d, %v", n, err)
	}
}

func TestRefreshSavesSnapshots(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	store := newFakeStore()
	uc := newTestUC(lister).WithStore(store)
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	built, _ := uc.Get(domain.IndexDow30)
	if store.saved[domain.IndexDow30] != built {
		t.Error("expected the built snapshot to be saved")
	}
}

func TestRefreshPublishesWhenSaveFails(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	store := newFakeStore()
	store.saveErr = errors.New("redis down")
	uc := newTestUC(lister).WithStore(store)
	if err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("expected the save failure to be reported")
	}
	if _, err := uc.Get(domain.IndexDow30); err != nil {
		t.Errorf("snapshot should still be served after a failed save: %v", err)
	}
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) add(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) kinds() []EventKind {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []EventKind
	for _, e := range l.events {
		if e.Kind != EventProgress {
			out = append(out, e.Kind)
		}
	}
	return out
}

func TestRefreshEmitsEvents(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}, {Symbol: "JPM", Sector: "Financials"}},
	}}
	log := &eventLog{}
	uc := newTestUC(lister).WithEvents(log.add)
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dow 30 builds; the other two indexes have no constituents but still build (empty maps).
	want := []EventKind{EventStarted, EventBuilt, EventStarted, EventBuilt, EventStarted, EventBuilt}
	if got := log.kinds(); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	built := log.events[1]
	if built.Index != domain.IndexDow30 || built.Stocks != 2 || built.MissingChanges != 1 || built.FirstChangeErr == nil {
		t.Errorf("unexpected built event %+v", built)
	}
}

func TestFailedBuildIsUnavailableNotWaiting(t *testing.T) {
	lister := &fakeLister{err: errors.New("upstream down")}
	log := &eventLog{}
	uc := newTestUC(lister).WithEvents(log.add)

	if _, err := uc.Get(domain.IndexSP500); !errors.Is(err, domain.ErrHeatmapNotReady) {
		t.Fatalf("before any build: expected ErrHeatmapNotReady, got %v", err)
	}
	if err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	_, err := uc.Get(domain.IndexSP500)
	if !errors.Is(err, domain.ErrHeatmapUnavailable) || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("after a failed build: expected ErrHeatmapUnavailable wrapping the cause, got %v", err)
	}
	if k := log.kinds(); len(k) != 3 || k[0] != EventFailed || log.events[0].ServingPrevious {
		t.Errorf("expected three EventFailed without a previous snapshot, got %+v", log.events)
	}
}

type slowChanger struct{ delay time.Duration }

func (s slowChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	time.Sleep(s.delay)
	return &stockdomain.PriceChange{Symbol: symbol}, nil
}

func TestBuildReportsProgressAndWaitingState(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	quoter := &fakeQuoter{quotes: map[string]domain.Quote{"AAPL": {Symbol: "AAPL", MarketCap: 1}}}
	log := &eventLog{}
	uc := NewHeatmapUseCase(lister, quoter, slowChanger{delay: 80 * time.Millisecond}).WithEvents(log.add)
	uc.progressEvery = 10 * time.Millisecond

	done := make(chan error)
	go func() { done <- uc.Refresh(context.Background()) }()
	time.Sleep(30 * time.Millisecond)
	if _, err := uc.Get(domain.IndexDow30); !errors.Is(err, domain.ErrHeatmapNotReady) {
		t.Errorf("mid-build: expected ErrHeatmapNotReady, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	var progress int
	for _, e := range log.events {
		if e.Kind == EventProgress && e.Index == domain.IndexDow30 && e.Symbols == 1 {
			progress++
		}
	}
	if progress == 0 {
		t.Error("expected at least one progress event during the slow build")
	}
}
