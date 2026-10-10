package cached

import (
	"time"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

// ProfileFetcher caches GetProfile by symbol. Profiles (name, sector, market
// cap) move slowly, so a long TTL saves most of the provider calls.
type ProfileFetcher struct {
	inner stockdomain.ProfileFetcher
	cache *cache.Keyed[string, *stockdomain.CompanyProfile]
}

func NewProfileFetcher(inner stockdomain.ProfileFetcher, ttl time.Duration) *ProfileFetcher {
	return &ProfileFetcher{inner: inner, cache: cache.NewKeyed[string, *stockdomain.CompanyProfile](ttl, 0)}
}

func (p *ProfileFetcher) GetProfile(symbol string) (*stockdomain.CompanyProfile, error) {
	return p.cache.Fill(symbol, func() (*stockdomain.CompanyProfile, error) {
		return p.inner.GetProfile(symbol)
	})
}

var _ stockdomain.ProfileFetcher = (*ProfileFetcher)(nil)
