// Package throttled provides rate-limiting decorators for domain interfaces
// backed by external market-data clients. Like the cached decorators, they are
// wrapped at the wiring seam (main.go).
package throttled

import (
	"context"

	"golang.org/x/time/rate"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

// NewLimiter allows perMinute calls per minute with no burst beyond one. Share
// one limiter between decorators to cap their combined call rate.
func NewLimiter(perMinute int) *rate.Limiter {
	return rate.NewLimiter(rate.Limit(float64(perMinute)/60), 1)
}

// wait blocks for a token. The domain interfaces carry no context; a wait is at
// most one token interval, so shutdown is never held up for long.
func wait(l *rate.Limiter) error {
	return l.Wait(context.Background())
}

// PriceChanger waits on a token bucket before every GetPriceChange, so a bulk
// job (the index heatmap refresh) cannot use up the provider's per-minute call
// budget that interactive requests share. Wrap it inside a cache so that only
// cache misses are throttled.
type PriceChanger struct {
	inner   stockdomain.PriceChanger
	limiter *rate.Limiter
}

func NewPriceChanger(inner stockdomain.PriceChanger, limiter *rate.Limiter) *PriceChanger {
	return &PriceChanger{inner: inner, limiter: limiter}
}

func (p *PriceChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	if err := wait(p.limiter); err != nil {
		return nil, err
	}
	return p.inner.GetPriceChange(symbol)
}

var _ stockdomain.PriceChanger = (*PriceChanger)(nil)

// ProfileFetcher is the GetProfile counterpart of PriceChanger.
type ProfileFetcher struct {
	inner   stockdomain.ProfileFetcher
	limiter *rate.Limiter
}

func NewProfileFetcher(inner stockdomain.ProfileFetcher, limiter *rate.Limiter) *ProfileFetcher {
	return &ProfileFetcher{inner: inner, limiter: limiter}
}

func (p *ProfileFetcher) GetProfile(symbol string) (*stockdomain.CompanyProfile, error) {
	if err := wait(p.limiter); err != nil {
		return nil, err
	}
	return p.inner.GetProfile(symbol)
}

var _ stockdomain.ProfileFetcher = (*ProfileFetcher)(nil)
