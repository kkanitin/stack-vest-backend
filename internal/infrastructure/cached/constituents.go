package cached

import (
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

// ConstituentLister caches ListConstituents per index. Index membership changes
// a few times a quarter, so a long TTL is fine.
type ConstituentLister struct {
	inner market.ConstituentLister
	cache *cache.Keyed[market.Index, []market.Constituent]
}

func NewConstituentLister(inner market.ConstituentLister, ttl time.Duration) *ConstituentLister {
	return &ConstituentLister{inner: inner, cache: cache.NewKeyed[market.Index, []market.Constituent](ttl, 0)}
}

func (l *ConstituentLister) ListConstituents(index market.Index) ([]market.Constituent, error) {
	return l.cache.Fill(index, func() ([]market.Constituent, error) {
		return l.inner.ListConstituents(index)
	})
}

var _ market.ConstituentLister = (*ConstituentLister)(nil)
