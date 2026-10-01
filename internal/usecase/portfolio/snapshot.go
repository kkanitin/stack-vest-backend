package portfolio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
)

// SnapshotValues records, for the given day, the total USD value of each user's
// holdings across all of their portfolios. It is meant to run repeatedly through the
// day: each run replaces that day's row, so a day settles on its last recorded value.
//
// A user is written only when every one of their holdings could be priced. A partial
// total would chart as a dip that never happened, so that user is skipped for this run
// and picked up by a later one. Pricing is quote-only — unlike fetchPrices it does not
// need the 30-day price change, so an outage of that lookup costs no history.
//
// It returns how many users were written and how many were skipped.
func (uc *UseCase) SnapshotValues(ctx context.Context, day time.Time) (written, skipped int, err error) {
	holdings, err := uc.repo.ListAllHoldings(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("list holdings: %w", err)
	}
	if len(holdings) == 0 {
		return 0, 0, nil
	}

	prices := uc.fetchQuotePrices(ctx, distinctHoldingSymbols(holdings))
	if err := ctx.Err(); err != nil {
		// Cancelled mid-pricing (shutdown): the price map is incomplete, so write nothing.
		return 0, 0, err
	}

	valueByUser := make(map[string]float64)
	unpriced := make(map[string]struct{})
	for _, h := range holdings {
		price, ok := prices[h.Symbol]
		if !ok {
			unpriced[h.UserID] = struct{}{}
			continue
		}
		valueByUser[h.UserID] += h.Shares * price
	}
	for userID := range unpriced {
		delete(valueByUser, userID)
	}

	if len(valueByUser) > 0 {
		if err := uc.repo.UpsertValueSnapshots(ctx, day, valueByUser); err != nil {
			return 0, len(unpriced), fmt.Errorf("upsert value snapshots: %w", err)
		}
	}
	return len(valueByUser), len(unpriced), nil
}

// GetValueHistory returns the user's recorded daily values for the range, oldest first.
// now anchors the look-back; a bounded range starts at 00:00 UTC that many days before it.
func (uc *UseCase) GetValueHistory(ctx context.Context, email string, r portfoliodomain.HistoryRange, now time.Time) (*portfoliodomain.ValueHistory, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}

	var from *time.Time
	if days, bounded := r.Days(); bounded {
		n := now.UTC()
		start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days)
		from = &start
	}

	points, err := uc.repo.GetValueHistory(ctx, user.ID, from)
	if err != nil {
		return nil, err
	}
	return &portfoliodomain.ValueHistory{Range: r, Points: points}, nil
}

// fetchQuotePrices concurrently fetches the current price of each symbol, bounded by
// fetchConcurrency. A symbol whose quote fails, or comes back with a non-positive price,
// is omitted from the returned map. It stops starting new lookups once ctx is cancelled.
func (uc *UseCase) fetchQuotePrices(ctx context.Context, symbols []string) map[string]float64 {
	out := make(map[string]float64, len(symbols))
	var mu sync.Mutex
	g := new(errgroup.Group)
	g.SetLimit(fetchConcurrency)
	for _, sym := range symbols {
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			q, err := uc.quoter.GetQuote(sym)
			if err != nil {
				zap.L().Warn("value snapshot: failed to get quote", zap.String("symbol", sym), zap.Error(err))
				return nil
			}
			if q.Price <= 0 {
				return nil
			}
			mu.Lock()
			out[sym] = q.Price
			mu.Unlock()
			return nil
		})
	}
	g.Wait()
	return out
}

// distinctHoldingSymbols returns the unique symbols across holdings so each ticker is
// quoted once, however many users or portfolios hold it.
func distinctHoldingSymbols(holdings []*portfoliodomain.UserHolding) []string {
	seen := make(map[string]struct{}, len(holdings))
	symbols := make([]string, 0, len(holdings))
	for _, h := range holdings {
		if _, ok := seen[h.Symbol]; ok {
			continue
		}
		seen[h.Symbol] = struct{}{}
		symbols = append(symbols, h.Symbol)
	}
	return symbols
}
