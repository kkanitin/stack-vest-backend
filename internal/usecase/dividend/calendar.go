package dividend

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

const (
	// defaultForward is how far past `from` the window reaches when the caller sends
	// no `to`.
	defaultForward = 75 * 24 * time.Hour

	// bucketLookback is how far before a month's first day its fill starts fetching.
	// The provider filters on ex-date, but the calendar places an event on its payment
	// date, which usually falls days to weeks after the ex-date. Fetching ex-dates
	// from 45 days before the month start to the month end (45 + 31 = 76 days at
	// most) catches those payouts; one whose ex-date is more than 45 days before the
	// month start is missed.
	bucketLookback = 45 * 24 * time.Hour

	// maxSpanDays caps `to − from` (a quarter, at its longest), so one request reads
	// at most five month buckets.
	maxSpanDays = 92

	// boundMonths is how many months before and after the current month a window may
	// reach. It bounds how many month buckets callers can make the cache fill.
	boundMonths = 13
)

// userFinder resolves the authenticated email to a user (for the user id).
type userFinder interface {
	FindByEmail(ctx context.Context, email string) (*userdomain.User, error)
}

// positionLister returns every position across all of a user's portfolios.
type positionLister interface {
	ListPositionsByUser(ctx context.Context, userID string) ([]*portfoliodomain.Position, error)
}

// transactionLister returns every ledger transaction across a user's portfolios.
type transactionLister interface {
	ListTransactionsByUser(ctx context.Context, userID string) ([]*portfoliodomain.Transaction, error)
}

// CalendarUseCase builds a user's dividend calendar for a date range by joining the
// market-wide dividend calendar against the user's holdings. The calendar is
// market-wide reference data, so it is cached once for everyone, in one blob per
// calendar month: any requested range is assembled from the months it overlaps, so
// users browsing different ranges share the same few keys instead of each range
// caching its own copy. Concurrent misses on a month are coalesced via singleflight,
// so a cold month triggers exactly one upstream fill.
type CalendarUseCase struct {
	users     userFinder
	positions positionLister
	fetcher   dividenddomain.Fetcher
	cache     dividenddomain.Cache
	ledger    transactionLister // optional; see WithLedger
	sf        singleflight.Group
}

// WithLedger makes past events use the shares held at the end of the day before the
// ex-date, read from the transaction ledger (loaded once per request). Without it every
// event uses the current share count. It is a setter so the constructor's callers stay
// unchanged; call it once at wiring time.
func (uc *CalendarUseCase) WithLedger(l transactionLister) *CalendarUseCase {
	uc.ledger = l
	return uc
}

func NewCalendarUseCase(
	users userFinder,
	positions positionLister,
	fetcher dividenddomain.Fetcher,
	cache dividenddomain.Cache,
) *CalendarUseCase {
	return &CalendarUseCase{
		users:     users,
		positions: positions,
		fetcher:   fetcher,
		cache:     cache,
	}
}

// Execute returns the dividend calendar entries for the user's holdings whose
// reference date (payment date, or ex-date when payment is unknown) falls within
// [from, to], sorted by reference date then symbol. from/to are honored as given,
// past dates included: a zero from defaults to today and a zero to defaults to
// from + 75 days. A window resolveWindow rejects returns its sentinel error before
// any I/O is done.
func (uc *CalendarUseCase) Execute(ctx context.Context, email string, from, to time.Time) ([]dividenddomain.CalendarEntry, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	from, to, err := resolveWindow(today, from, to)
	if err != nil {
		return nil, err
	}

	user, err := uc.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	positions, err := uc.positions.ListPositionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	// Aggregate shares per symbol: the same ticker may be held in several
	// portfolios, and estimated payout is over the user's total exposure.
	sharesBySymbol := make(map[string]float64)
	for _, pos := range positions {
		sharesBySymbol[pos.Symbol] += pos.Shares
	}
	held, err := uc.loadHeldShares(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if len(sharesBySymbol) == 0 && held.empty() {
		return []dividenddomain.CalendarEntry{}, nil
	}

	entries := make([]dividenddomain.CalendarEntry, 0)
	for _, month := range monthsIn(from, to) {
		// A fill cannot be cancelled once started, but an abandoned request must not
		// go on to fill the rest of its window.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		events, err := uc.bucket(ctx, month)
		if err != nil {
			return nil, err
		}

		// Keep only the part of the window that lies in this month. A bucket should
		// hold nothing else anyway, but clamping here is what guarantees an event is
		// never emitted by two buckets.
		lo, hi := month, monthEnd(month)
		if from.After(lo) {
			lo = from
		}
		if to.Before(hi) {
			hi = to
		}

		for _, ev := range events {
			ref := referenceDate(ev)
			if ref.Before(lo) || ref.After(hi) {
				continue
			}
			// Future events (ex-date after today) use the current total shares; past ones
			// use the shares held at the end of the day before the ex-date, so a position
			// opened after the ex-date earns nothing and one sold since still counts.
			// Amounts are an estimate and sum across currencies naively.
			shares := sharesBySymbol[ev.Symbol]
			if exDate := exDateOf(ev); !exDate.After(today) {
				if h, ok := held.sharesBefore(ev.Symbol, exDate); ok {
					shares = h
				}
			}
			if shares <= 0 {
				continue
			}
			entries = append(entries, dividenddomain.CalendarEntry{
				Event:           ev,
				Shares:          shares,
				EstimatedAmount: shares * ev.Dividend,
			})
		}
	}

	// Stable: a symbol can have several rows on one date, and their order must not
	// shuffle between requests (it would move rows across page boundaries).
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := referenceDate(entries[i].Event), referenceDate(entries[j].Event)
		if ri.Equal(rj) {
			return entries[i].Symbol < entries[j].Symbol
		}
		return ri.Before(rj)
	})
	return entries, nil
}

// exDateOf is the event's ex-dividend date, or its reference date when the ex-date is
// unknown.
func exDateOf(ev dividenddomain.Event) time.Time {
	if !ev.ExDate.IsZero() {
		return ev.ExDate
	}
	return referenceDate(ev)
}

// ledgerShares answers "how many shares of a symbol did the user hold at the end of the
// day before a date", summed over all portfolios, from one load of the ledger.
type ledgerShares struct {
	bySymbol map[string][]*portfoliodomain.Transaction
}

func (l ledgerShares) empty() bool { return len(l.bySymbol) == 0 }

// sharesBefore returns the shares held at the end of the day before date. ok is false
// when the ledger has no transactions for the symbol.
func (l ledgerShares) sharesBefore(symbol string, date time.Time) (float64, bool) {
	txs, ok := l.bySymbol[symbol]
	if !ok {
		return 0, false
	}
	cutoff := date.UTC().Format(portfoliodomain.DateLayout)
	var shares float64
	for _, t := range txs {
		if t.Date >= cutoff {
			continue
		}
		if t.Side == portfoliodomain.SideSell {
			shares -= t.Quantity
		} else {
			shares += t.Quantity
		}
	}
	return portfoliodomain.Round8(shares), true
}

func (uc *CalendarUseCase) loadHeldShares(ctx context.Context, userID string) (ledgerShares, error) {
	out := ledgerShares{bySymbol: map[string][]*portfoliodomain.Transaction{}}
	if uc.ledger == nil {
		return out, nil
	}
	txs, err := uc.ledger.ListTransactionsByUser(ctx, userID)
	if err != nil {
		return out, fmt.Errorf("list transactions: %w", err)
	}
	for _, t := range txs {
		out.bySymbol[t.Symbol] = append(out.bySymbol[t.Symbol], t)
	}
	return out, nil
}

// resolveWindow applies the defaults to a requested window and validates it. A zero
// from becomes today and a zero to becomes from + defaultForward, capped at the
// latest allowed day so a default never pushes an in-bounds from out of bounds. It
// returns ErrInvalidRange when to is before from (which can also arise from the
// defaults: only a to, earlier than today), ErrRangeTooLong when the span exceeds
// maxSpanDays, and ErrRangeOutOfBounds when the window reaches outside the months
// from boundMonths before to boundMonths after today's month.
func resolveWindow(today, from, to time.Time) (time.Time, time.Time, error) {
	// Both bounds are derived from the first day of today's month. AddDate on a later
	// day normalises an overflow into the following month (Oct 31 − 13 months is
	// "Sep 31", i.e. Oct 1), which would shift the bounds on some days of the month.
	thisMonth := monthStart(today)
	earliest := thisMonth.AddDate(0, -boundMonths, 0)
	latest := monthEnd(thisMonth.AddDate(0, boundMonths, 0))

	if from.IsZero() {
		from = today
	}
	if to.IsZero() {
		to = from.Add(defaultForward)
		if to.After(latest) && !from.After(latest) {
			to = latest
		}
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, dividenddomain.ErrInvalidRange
	}
	if to.Sub(from) > maxSpanDays*24*time.Hour {
		return time.Time{}, time.Time{}, dividenddomain.ErrRangeTooLong
	}
	if from.Before(earliest) || to.After(latest) {
		return time.Time{}, time.Time{}, dividenddomain.ErrRangeOutOfBounds
	}
	return from, to, nil
}

// monthStart returns the first day of t's calendar month, at midnight UTC.
func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// monthEnd returns the last day of the calendar month that starts on month (a
// monthStart value).
func monthEnd(month time.Time) time.Time {
	return month.AddDate(0, 1, -1)
}

// monthsIn returns the first day of every calendar month overlapping [from, to], in
// ascending order.
func monthsIn(from, to time.Time) []time.Time {
	var months []time.Time
	for m := monthStart(from); !m.After(to); m = m.AddDate(0, 1, 0) {
		months = append(months, m)
	}
	return months
}

// bucketKey identifies a month's cached bucket (the cache implementation adds its
// own versioned prefix). The key carries no fetch date, so a bucket is refreshed by
// its TTL rather than by rotating keys.
func bucketKey(month time.Time) string {
	return "calendar:" + month.Format("2006-01")
}

// bucket returns the market-wide dividend events whose reference date falls in the
// calendar month starting on month, serving from cache when present and otherwise
// filling from the provider. A fill fetches ex-dates in [month − bucketLookback,
// month end] and keeps the events that belong to the month. It is wrapped in
// singleflight so concurrent misses collapse into one upstream fill.
func (uc *CalendarUseCase) bucket(ctx context.Context, month time.Time) ([]dividenddomain.Event, error) {
	key := bucketKey(month)
	if events, ok, err := uc.cache.Get(ctx, key); err != nil {
		zap.L().Warn("dividend cache read failed", logger.RequestID(ctx), zap.String("key", key), zap.Error(err))
	} else if ok {
		return events, nil
	}

	v, err, _ := uc.sf.Do(key, func() (any, error) {
		end := monthEnd(month)
		fetched, err := uc.fetcher.GetDividendsCalendar(month.Add(-bucketLookback), end)
		if err != nil {
			return nil, err
		}

		// A new slice, never fetched[:0]: the fetched slice belongs to the fetcher.
		// Non-nil even when empty, so an empty month is negative-cached as a hit.
		events := make([]dividenddomain.Event, 0)
		for _, ev := range fetched {
			if ref := referenceDate(ev); !ref.Before(month) && !ref.After(end) {
				events = append(events, ev)
			}
		}

		// The fetch above cannot be cancelled, so by now the request that started it
		// may be gone. Store the result anyway (detached from that request's
		// cancellation) rather than throw the provider calls away.
		if err := uc.cache.Set(context.WithoutCancel(ctx), key, events); err != nil {
			zap.L().Warn("dividend cache write failed", logger.RequestID(ctx), zap.String("key", key), zap.Error(err))
		}
		return events, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]dividenddomain.Event), nil
}

// referenceDate is the date the calendar sorts and filters on: the payment date
// when known, otherwise the ex-dividend date.
func referenceDate(ev dividenddomain.Event) time.Time {
	if !ev.PaymentDate.IsZero() {
		return ev.PaymentDate
	}
	return ev.ExDate
}
