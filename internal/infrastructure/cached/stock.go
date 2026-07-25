// Package cached provides caching decorators for domain interfaces backed by
// external market-data clients. Wrapping happens at the wiring seam (main.go)
// so use cases keep depending on the plain domain interface and are unaware
// caching exists.
package cached

import (
	"time"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

// Quoter caches GetQuote by symbol. Symbol cardinality is bounded by the
// tradable market, not caller input, so it's constructed with an unbounded
// cache.Keyed (maxSize 0).
type Quoter struct {
	inner stockdomain.Quoter
	cache *cache.Keyed[string, *stockdomain.Quote]
}

func NewQuoter(inner stockdomain.Quoter, ttl time.Duration) *Quoter {
	return &Quoter{inner: inner, cache: cache.NewKeyed[string, *stockdomain.Quote](ttl, 0)}
}

func (q *Quoter) GetQuote(symbol string) (*stockdomain.Quote, error) {
	return q.cache.Fill(symbol, func() (*stockdomain.Quote, error) {
		return q.inner.GetQuote(symbol)
	})
}

var _ stockdomain.Quoter = (*Quoter)(nil)

// PriceChanger caches GetPriceChange by symbol.
type PriceChanger struct {
	inner stockdomain.PriceChanger
	cache *cache.Keyed[string, *stockdomain.PriceChange]
}

func NewPriceChanger(inner stockdomain.PriceChanger, ttl time.Duration) *PriceChanger {
	return &PriceChanger{inner: inner, cache: cache.NewKeyed[string, *stockdomain.PriceChange](ttl, 0)}
}

func (p *PriceChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	return p.cache.Fill(symbol, func() (*stockdomain.PriceChange, error) {
		return p.inner.GetPriceChange(symbol)
	})
}

var _ stockdomain.PriceChanger = (*PriceChanger)(nil)
