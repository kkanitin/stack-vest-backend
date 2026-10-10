package throttled

import (
	"testing"
	"time"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type stub struct{}

func (stub) GetPriceChange(s string) (*stockdomain.PriceChange, error) {
	return &stockdomain.PriceChange{Symbol: s}, nil
}
func (stub) GetProfile(s string) (*stockdomain.CompanyProfile, error) {
	return &stockdomain.CompanyProfile{Symbol: s}, nil
}

func TestDecoratorsShareOneLimiter(t *testing.T) {
	limiter := NewLimiter(600) // one token every 100ms
	pc := NewPriceChanger(stub{}, limiter)
	pf := NewProfileFetcher(stub{}, limiter)

	start := time.Now()
	for range 2 {
		if _, err := pc.GetPriceChange("AAPL"); err != nil {
			t.Fatal(err)
		}
		if _, err := pf.GetProfile("AAPL"); err != nil {
			t.Fatal(err)
		}
	}
	// Four calls on one 10/s bucket with burst 1 take at least ~300ms; separate
	// limiters would allow it in ~100ms.
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("calls were not rate-limited together: %s", elapsed)
	}
}
