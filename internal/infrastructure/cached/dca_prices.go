package cached

import (
	"strings"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

// dcaPricesMaxEntries bounds the cache: the key includes caller-chosen dates, so the
// key space is not naturally small. Reaching it clears the cache (see cache.Keyed).
const dcaPricesMaxEntries = 500

type dcaPriceKey struct {
	symbol   string
	from, to string
}

// DCAPrices caches GetHistoricalPrices per (symbol, from, to). The DCA simulator uses
// adjusted closes over ranges of up to 30 years, so HistoryCloser (plain closes, five-year
// window) cannot serve it. Keying on the exact range means a hit returns exactly what the
// provider returned, and changing only the amount or frequency never reaches the provider.
//
// Concurrent misses for a key are coalesced into one fetch. Errors are not cached, so a
// failed fetch is retried by the next caller. An empty result is cached like any other.
type DCAPrices struct {
	inner dca.PriceFetcher
	cache *cache.Keyed[dcaPriceKey, []dca.HistoricalPrice]
}

func NewDCAPrices(inner dca.PriceFetcher, ttl time.Duration) *DCAPrices {
	return &DCAPrices{
		inner: inner,
		cache: cache.NewKeyed[dcaPriceKey, []dca.HistoricalPrice](ttl, dcaPricesMaxEntries),
	}
}

// GetHistoricalPrices returns the prices for symbol within [from, to]. The returned
// slice is a copy the caller may keep or modify.
func (p *DCAPrices) GetHistoricalPrices(symbol string, from, to time.Time) ([]dca.HistoricalPrice, error) {
	key := dcaPriceKey{
		symbol: strings.ToUpper(symbol),
		from:   from.Format("2006-01-02"),
		to:     to.Format("2006-01-02"),
	}
	prices, err := p.cache.Fill(key, func() ([]dca.HistoricalPrice, error) {
		return p.inner.GetHistoricalPrices(symbol, from, to)
	})
	if err != nil {
		return nil, err
	}
	return append([]dca.HistoricalPrice(nil), prices...), nil
}

var _ dca.PriceFetcher = (*DCAPrices)(nil)
