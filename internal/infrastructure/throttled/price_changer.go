// Package throttled provides rate-limiting decorators for domain interfaces
// backed by external market-data clients. Like the cached decorators, they are
// wrapped at the wiring seam (main.go).
package throttled

import (
	"context"

	"golang.org/x/time/rate"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

// PriceChanger waits on a token bucket before every GetPriceChange, so a bulk
// job (the index heatmap refresh) cannot use up the provider's per-minute call
// budget that interactive requests share. Wrap it inside a cache so that only
// cache misses are throttled.
type PriceChanger struct {
	inner   stockdomain.PriceChanger
	limiter *rate.Limiter
}

// NewPriceChanger allows perMinute calls per minute with no burst beyond one.
func NewPriceChanger(inner stockdomain.PriceChanger, perMinute int) *PriceChanger {
	return &PriceChanger{inner: inner, limiter: rate.NewLimiter(rate.Limit(float64(perMinute)/60), 1)}
}

func (p *PriceChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	// The domain interface carries no context; a wait is at most one token
	// interval, so shutdown is never held up for long.
	if err := p.limiter.Wait(context.Background()); err != nil {
		return nil, err
	}
	return p.inner.GetPriceChange(symbol)
}

var _ stockdomain.PriceChanger = (*PriceChanger)(nil)
