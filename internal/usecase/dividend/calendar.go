package dividend

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"golang.org/x/sync/singleflight"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

// fetchLookback / fetchForward define the market window fetched and cached. The
// display filter keys on paymentDate, but FMP's calendar from/to may filter on
// ex-date; the lookback captures dividends that have already gone ex but not yet
// paid, so those imminent payouts are not dropped regardless of FMP's filter axis.
// lookback+forward (= ~89 days) stays within the provider's 3-month range cap.
const (
	fetchLookback = 14 * 24 * time.Hour
	fetchForward  = 75 * 24 * time.Hour
)

// userFinder resolves the authenticated email to a user (for the user id).
type userFinder interface {
	FindByEmail(ctx context.Context, email string) (*userdomain.User, error)
}

// positionLister returns every position across all of a user's portfolios.
type positionLister interface {
	ListPositionsByUser(ctx context.Context, userID string) ([]*portfoliodomain.Position, error)
}

// CalendarUseCase builds a user's upcoming-dividend calendar by fetching the
// market-wide dividend calendar once (shared across all users via the cache) and
// joining it against the user's holdings. The calendar is market-wide reference
// data, so a single cached blob serves everyone; concurrent misses are coalesced
// via singleflight so a cold cache triggers exactly one upstream call.
type CalendarUseCase struct {
	users     userFinder
	positions positionLister
	fetcher   dividenddomain.Fetcher
	cache     dividenddomain.Cache
	sf        singleflight.Group
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
// [from, to]. The displayable window is fixed to roughly [today, today+75d]: a zero
// from/to defaults to it, and a caller-supplied from/to is clamped into it (a
// request outside this fixed forward window yields the available subset, not an
// error — see the handler note).
func (uc *CalendarUseCase) Execute(ctx context.Context, email string, from, to time.Time) ([]dividenddomain.CalendarEntry, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	fetchFrom := today.Add(-fetchLookback)
	fetchTo := today.Add(fetchForward)

	// Display window: never show already-paid dividends (floor at today) and never
	// claim data beyond what was fetched (ceil at fetchTo).
	if from.IsZero() || from.Before(today) {
		from = today
	}
	if to.IsZero() || to.After(fetchTo) {
		to = fetchTo
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
	if len(sharesBySymbol) == 0 {
		return []dividenddomain.CalendarEntry{}, nil
	}

	events, err := uc.calendar(ctx, fetchFrom, fetchTo)
	if err != nil {
		return nil, err
	}

	entries := make([]dividenddomain.CalendarEntry, 0)
	for _, ev := range events {
		shares, held := sharesBySymbol[ev.Symbol]
		if !held {
			continue
		}
		ref := referenceDate(ev)
		if ref.Before(from) || ref.After(to) {
			continue
		}
		// MVP estimate: current total shares × per-share dividend. It does not check
		// ex-date eligibility (a position opened after the ex-date wouldn't actually
		// receive this dividend — Position.AddedAt could refine this later) and sums
		// across currencies naively.
		entries = append(entries, dividenddomain.CalendarEntry{
			Event:           ev,
			Shares:          shares,
			EstimatedAmount: shares * ev.Dividend,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		ri, rj := referenceDate(entries[i].Event), referenceDate(entries[j].Event)
		if ri.Equal(rj) {
			return entries[i].Symbol < entries[j].Symbol
		}
		return ri.Before(rj)
	})
	return entries, nil
}

// calendar returns the market-wide dividend calendar for [from, to], serving from
// cache when present and otherwise filling from the provider. The fill is wrapped
// in singleflight so concurrent misses collapse into one upstream call.
func (uc *CalendarUseCase) calendar(ctx context.Context, from, to time.Time) ([]dividenddomain.Event, error) {
	key := cacheKey(from, to)
	if events, ok, err := uc.cache.Get(ctx, key); err != nil {
		slog.WarnContext(ctx, "dividend cache read failed", "key", key, "error", err)
	} else if ok {
		return events, nil
	}

	v, err, _ := uc.sf.Do(key, func() (any, error) {
		events, err := uc.fetcher.GetDividendsCalendar(from, to)
		if err != nil {
			return nil, err
		}
		if err := uc.cache.Set(ctx, key, events); err != nil {
			slog.WarnContext(ctx, "dividend cache write failed", "key", key, "error", err)
		}
		return events, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]dividenddomain.Event), nil
}

// cacheKey identifies a fetched calendar window. Since the window is derived from
// today, the date stamp rotates the key daily (a fresh fetch each day).
func cacheKey(from, to time.Time) string {
	return fmt.Sprintf("calendar:%s:%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
}

// referenceDate is the date the calendar sorts and filters on: the payment date
// when known, otherwise the ex-dividend date.
func referenceDate(ev dividenddomain.Event) time.Time {
	if !ev.PaymentDate.IsZero() {
		return ev.PaymentDate
	}
	return ev.ExDate
}
