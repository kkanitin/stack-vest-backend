package constituents

import (
	"errors"
	"testing"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

func TestStaticListsLoad(t *testing.T) {
	want := map[market.Index][2]int{
		market.IndexSP500:     {480, 510},
		market.IndexNasdaq100: {95, 105},
		market.IndexDow30:     {30, 30},
	}
	for idx, bounds := range want {
		list, err := StaticLister{}.ListConstituents(idx)
		if err != nil {
			t.Fatalf("%s: %v", idx, err)
		}
		if len(list) < bounds[0] || len(list) > bounds[1] {
			t.Errorf("%s: %d symbols, want %d-%d", idx, len(list), bounds[0], bounds[1])
		}
		seen := map[string]bool{}
		for _, c := range list {
			if seen[c.Symbol] {
				t.Errorf("%s: duplicate %s", idx, c.Symbol)
			}
			seen[c.Symbol] = true
			if c.Symbol == "GOOG" || c.Symbol == "BRK.B" {
				t.Errorf("%s: unexpected symbol %s (secondary share class or dotted ticker)", idx, c.Symbol)
			}
		}
	}
	if _, err := (StaticLister{}).ListConstituents("ftse"); !errors.Is(err, market.ErrUnknownIndex) {
		t.Errorf("expected ErrUnknownIndex, got %v", err)
	}
}

type stubLister struct {
	calls int
	list  []market.Constituent
	err   error
}

func (s *stubLister) ListConstituents(market.Index) ([]market.Constituent, error) {
	s.calls++
	return s.list, s.err
}

func TestFallbackSwitchesOnPlanRestrictionAndStays(t *testing.T) {
	primary := &stubLister{err: market.ErrPlanRestricted}
	fallback := &stubLister{list: []market.Constituent{{Symbol: "AAPL"}}}
	var notified int
	l := NewFallbackLister(primary, fallback, func(market.Index, error) { notified++ })

	for range 3 {
		got, err := l.ListConstituents(market.IndexDow30)
		if err != nil || len(got) != 1 || got[0].Symbol != "AAPL" {
			t.Fatalf("expected the fallback list, got %v, %v", got, err)
		}
	}
	if primary.calls != 1 {
		t.Errorf("primary should be tried once per index, got %d calls", primary.calls)
	}
	if notified != 1 {
		t.Errorf("onFallback should fire once, got %d", notified)
	}
}

func TestFallbackKeepsPrimaryResultsAndOtherErrors(t *testing.T) {
	fallback := &stubLister{list: []market.Constituent{{Symbol: "AAPL"}}}

	live := &stubLister{list: []market.Constituent{{Symbol: "MSFT"}}}
	if got, _ := NewFallbackLister(live, fallback, nil).ListConstituents(market.IndexSP500); got[0].Symbol != "MSFT" {
		t.Errorf("expected the live list, got %v", got)
	}

	down := &stubLister{err: errors.New("timeout")}
	if _, err := NewFallbackLister(down, fallback, nil).ListConstituents(market.IndexSP500); err == nil || fallback.calls != 0 {
		t.Errorf("a non-plan error should be returned, not hidden by the fallback (err=%v, fallback calls=%d)", err, fallback.calls)
	}
}
