package constituents

import (
	"errors"
	"sync"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

// FallbackLister asks primary (the provider's live lists) first. Once primary
// answers that the endpoint is not on the plan, it switches to fallback for
// that index for good and calls onFallback once, so the switch can be logged.
// Any other primary error is returned as is.
type FallbackLister struct {
	primary    market.ConstituentLister
	fallback   market.ConstituentLister
	onFallback func(index market.Index, cause error)

	mu         sync.Mutex
	restricted map[market.Index]bool
}

func NewFallbackLister(primary, fallback market.ConstituentLister, onFallback func(market.Index, error)) *FallbackLister {
	return &FallbackLister{primary: primary, fallback: fallback, onFallback: onFallback, restricted: map[market.Index]bool{}}
}

func (l *FallbackLister) ListConstituents(index market.Index) ([]market.Constituent, error) {
	l.mu.Lock()
	restricted := l.restricted[index]
	l.mu.Unlock()
	if !restricted {
		list, err := l.primary.ListConstituents(index)
		if !errors.Is(err, market.ErrPlanRestricted) {
			return list, err
		}
		l.mu.Lock()
		first := !l.restricted[index]
		l.restricted[index] = true
		l.mu.Unlock()
		if first && l.onFallback != nil {
			l.onFallback(index, err)
		}
	}
	return l.fallback.ListConstituents(index)
}

var _ market.ConstituentLister = (*FallbackLister)(nil)
