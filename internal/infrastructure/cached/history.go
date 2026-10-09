package cached

import (
	"time"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

const (
	// historyWindow is how far back the full-window fetch reaches: five years plus a
	// 14-day pad so the first day of a five-year request still has an earlier close to
	// carry forward over weekends and holidays.
	historyWindowYears = 5
	historyWindowPad   = 14 * 24 * time.Hour
)

// HistoryCloser caches GetHistoryClose per symbol. Callers ask for many different
// [from, to] windows, so the cache does not key on them: on a miss it always fetches
// the full window [today - 5y - 14d, today] and every call clips that series to its own
// from/to in memory. One upstream call then serves every range for the symbol.
//
// Concurrent misses for a symbol are coalesced into one fetch (cache.Keyed.Fill).
// Errors are not cached, so a failed fetch is retried by the next caller. A request
// reaching back beyond the five-year window gets only the part inside it.
type HistoryCloser struct {
	inner stockdomain.HistoryFetcher
	cache *cache.Keyed[string, []stockdomain.HistoryPoint]
	now   func() time.Time
}

// NewHistoryCloser wraps inner. Symbol cardinality is bounded (callers pass configured
// symbols), so the cache is unbounded (maxSize 0).
func NewHistoryCloser(inner stockdomain.HistoryFetcher, ttl time.Duration) *HistoryCloser {
	return &HistoryCloser{
		inner: inner,
		cache: cache.NewKeyed[string, []stockdomain.HistoryPoint](ttl, 0),
		now:   time.Now,
	}
}

// GetHistoryClose returns the cached closes for symbol within [from, to] inclusive,
// oldest first. The returned slice is a copy the caller may keep.
func (h *HistoryCloser) GetHistoryClose(symbol string, from, to time.Time) ([]stockdomain.HistoryPoint, error) {
	all, err := h.cache.Fill(symbol, func() ([]stockdomain.HistoryPoint, error) {
		end := h.now().UTC()
		start := end.AddDate(-historyWindowYears, 0, 0).Add(-historyWindowPad)
		return h.inner.GetHistoryClose(symbol, start, end)
	})
	if err != nil {
		return nil, err
	}

	// Dates are YYYY-MM-DD, so string comparison is chronological.
	lo, hi := from.Format("2006-01-02"), to.Format("2006-01-02")
	out := make([]stockdomain.HistoryPoint, 0, len(all))
	for _, p := range all {
		if p.Date >= lo && p.Date <= hi {
			out = append(out, p)
		}
	}
	return out, nil
}

var _ stockdomain.HistoryFetcher = (*HistoryCloser)(nil)
