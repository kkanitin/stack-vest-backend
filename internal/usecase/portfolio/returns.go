package portfolio

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

// twrWindowDays is the look-back of the headline return on the summaries.
const twrWindowDays = 30

const dateLayout = portfoliodomain.DateLayout

// utcDay truncates t to 00:00 UTC of its calendar day.
func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ledgerRealised sums realised P&L across the transactions, replaying each
// (portfolio, symbol) ledger on its own. A ledger that fails to replay is skipped.
func ledgerRealised(txs []*portfoliodomain.Transaction) float64 {
	groups := make(map[string][]*portfoliodomain.Transaction)
	for _, t := range txs {
		k := t.PortfolioID + "|" + t.Symbol
		groups[k] = append(groups[k], t)
	}
	var total float64
	for _, g := range groups {
		res, err := portfoliodomain.Replay(g)
		if err != nil {
			zap.L().Warn("realised pnl: ledger replay failed", zap.Error(err))
			continue
		}
		total += res.RealisedPnl
	}
	return portfoliodomain.Round8(total)
}

// unrealisedPnl sums shares*(price-avgCost) over positions that have a live price.
func unrealisedPnl(positions []*portfoliodomain.Position, prices map[string]float64) float64 {
	var total float64
	for _, p := range positions {
		price, ok := prices[p.Symbol]
		if !ok || p.Shares <= 0 {
			continue
		}
		total += p.Shares * (price - p.AvgCost)
	}
	return total
}

// activeSymbols returns the symbols that were held at the end of day from or traded
// after it, i.e. the ones that can affect a series starting at from.
func activeSymbols(txs []*portfoliodomain.Transaction, from time.Time) []string {
	fromStr := from.Format(dateLayout)
	net := make(map[string]float64)
	touched := make(map[string]bool)
	for _, t := range txs {
		if t.Date > fromStr {
			touched[t.Symbol] = true
			continue
		}
		if t.Side == portfoliodomain.SideSell {
			net[t.Symbol] -= t.Quantity
		} else {
			net[t.Symbol] += t.Quantity
		}
	}
	for s, n := range net {
		if n > 1e-9 {
			touched[s] = true
		}
	}
	out := make([]string, 0, len(touched))
	for s := range touched {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// loadCloses fetches daily closes for the symbols concurrently. A symbol whose fetch
// fails (or when no fetcher is configured) is omitted, so it is treated as unpriced.
func (uc *UseCase) loadCloses(ctx context.Context, symbols []string, from, to time.Time) map[string][]stockdomain.HistoryPoint {
	out := make(map[string][]stockdomain.HistoryPoint, len(symbols))
	if uc.benchmarkFetcher == nil {
		return out
	}
	var mu sync.Mutex
	g := new(errgroup.Group)
	g.SetLimit(fetchConcurrency)
	for _, sym := range symbols {
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			pts, err := uc.benchmarkFetcher.GetHistoryClose(sym, from, to)
			if err != nil {
				zap.L().Warn("returns: closes unavailable", logger.RequestID(ctx), zap.String("symbol", sym), zap.Error(err))
				return nil
			}
			sort.Slice(pts, func(i, j int) bool { return pts[i].Date < pts[j].Date })
			mu.Lock()
			out[sym] = pts
			mu.Unlock()
			return nil
		})
	}
	g.Wait()
	return out
}

type symbolState struct {
	shares    float64
	price     float64
	hasPrice  bool
	wasPriced bool // true once the symbol has been valued on some day of the series
	next      int  // index into closes
}

// buildSeries replays the transactions against forward-filled daily closes and returns
// one DailyPoint per UTC day from..to inclusive.
//
//   - A buy is a flow of qty*price+fee, a sell of -(qty*price-fee). An opening entry is
//     a flow at that day's market close (not its average cost), so a carried-over
//     holding does not read as a one-day gain.
//   - live, when non-empty, overrides the price on the last day (today's quote).
//   - A symbol with no price on a day is left out of that day's value and flow. On the
//     first day it can be priced, its whole value enters as a flow, so it never reads as
//     a gain either.
func buildSeries(
	txs []*portfoliodomain.Transaction, closes map[string][]stockdomain.HistoryPoint,
	live map[string]float64, from, to time.Time,
) []portfoliodomain.DailyPoint {
	from, to = utcDay(from), utcDay(to)
	if to.Before(from) {
		return nil
	}

	sorted := make([]*portfoliodomain.Transaction, len(txs))
	copy(sorted, txs)
	// Same order as Replay (date, buys before sells, created_at): ListTransactionsByUser
	// returns newest first, so sorting by date alone would apply a day's sells before its
	// buys and corrupt the held-share series.
	sort.SliceStable(sorted, func(i, j int) bool { return portfoliodomain.TransactionBefore(sorted[i], sorted[j]) })

	states := make(map[string]*symbolState)
	for _, t := range sorted {
		if states[t.Symbol] == nil {
			states[t.Symbol] = &symbolState{}
		}
	}
	apply := func(s *symbolState, t *portfoliodomain.Transaction) {
		if t.Side == portfoliodomain.SideSell {
			s.shares -= t.Quantity
		} else {
			s.shares += t.Quantity
		}
		s.shares = portfoliodomain.Round8(s.shares)
		// Defensive only: writes are replay-checked and the order above matches Replay,
		// so shares cannot go negative.
		if s.shares < 0 {
			s.shares = 0
		}
	}

	// Holdings carried into the first day.
	ti := 0
	fromStr := from.Format(dateLayout)
	for ti < len(sorted) && sorted[ti].Date < fromStr {
		apply(states[sorted[ti].Symbol], sorted[ti])
		ti++
	}

	var series []portfoliodomain.DailyPoint
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format(dateLayout)
		isLast := d.Equal(to)

		for sym, s := range states {
			pts := closes[sym]
			for s.next < len(pts) && pts[s.next].Date <= ds {
				if pts[s.next].Close > 0 {
					s.price, s.hasPrice = pts[s.next].Close, true
				}
				s.next++
			}
			if isLast {
				if p := live[sym]; p > 0 {
					s.price, s.hasPrice = p, true
				}
			}
		}

		dayFlow := make(map[string]float64)
		for ti < len(sorted) && sorted[ti].Date <= ds {
			t := sorted[ti]
			s := states[t.Symbol]
			switch {
			case t.Side == portfoliodomain.SideSell:
				dayFlow[t.Symbol] -= t.Quantity*t.Price - t.Fee
			case t.IsOpening:
				p := t.Price
				if s.hasPrice {
					p = s.price
				}
				dayFlow[t.Symbol] += t.Quantity * p
			default:
				dayFlow[t.Symbol] += t.Quantity*t.Price + t.Fee
			}
			apply(s, t)
			ti++
		}

		var value, flow float64
		for sym, s := range states {
			if !s.hasPrice {
				continue
			}
			value += s.shares * s.price
			if s.wasPriced {
				flow += dayFlow[sym]
			} else {
				flow += s.shares * s.price
				s.wasPriced = true
			}
		}
		series = append(series, portfoliodomain.DailyPoint{Date: ds, Value: value, Flow: flow})
	}
	return series
}

// seriesFor builds the daily series for txs over from..to, fetching closes for the
// symbols that matter. live prices apply to the last day.
func (uc *UseCase) seriesFor(
	ctx context.Context, txs []*portfoliodomain.Transaction, live map[string]float64, from, to time.Time,
) []portfoliodomain.DailyPoint {
	if len(txs) == 0 {
		return nil
	}
	closes := uc.loadCloses(ctx, activeSymbols(txs, utcDay(from)), from.Add(-benchmarkLookback), to)
	return buildSeries(txs, closes, live, from, to)
}

// windowReturn returns the 30-day time-weighted return (percent) and the money gained
// over the window excluding deposits and withdrawals, ending at now. live carries
// today's quotes. Anything that cannot be priced leaves the figures at 0.
func (uc *UseCase) windowReturn(
	ctx context.Context, txs []*portfoliodomain.Transaction, live map[string]float64, now time.Time,
) (pct, gain float64) {
	to := utcDay(now)
	series := uc.seriesFor(ctx, txs, live, to.AddDate(0, 0, -twrWindowDays), to)
	if len(series) < 2 {
		return 0, 0
	}
	return portfoliodomain.TWR(series) * 100, portfoliodomain.Gain(series)
}

// attachReturns sets ReturnPct on each point to the cumulative time-weighted return from
// the first point. It never fails the request: without a ledger, a history fetcher or
// prices, the points keep ReturnPct 0.
func (uc *UseCase) attachReturns(ctx context.Context, userID string, points []*portfoliodomain.ValuePoint, now time.Time) {
	if len(points) < 2 {
		return
	}
	txs, err := uc.repo.ListTransactionsByUser(ctx, userID)
	if err != nil {
		zap.L().Warn("returns: list transactions failed", logger.RequestID(ctx), zap.Error(err))
		return
	}
	if len(txs) == 0 {
		return
	}
	from, err1 := time.Parse(dateLayout, points[0].Date)
	to, err2 := time.Parse(dateLayout, points[len(points)-1].Date)
	if err1 != nil || err2 != nil {
		return
	}

	var live map[string]float64
	if !to.Before(utcDay(now)) {
		live = uc.fetchQuotePrices(ctx, heldNowSymbols(txs))
	}
	series := uc.seriesFor(ctx, txs, live, from, to)
	if len(series) == 0 {
		return
	}
	cum := portfoliodomain.CumulativeTWR(series)
	byDate := make(map[string]float64, len(series))
	for i, p := range series {
		byDate[p.Date] = cum[i] * 100
	}
	for _, p := range points {
		p.ReturnPct = byDate[p.Date]
	}
}

// heldNowSymbols returns the symbols with a positive net quantity across txs.
func heldNowSymbols(txs []*portfoliodomain.Transaction) []string {
	net := make(map[string]float64)
	for _, t := range txs {
		if t.Side == portfoliodomain.SideSell {
			net[t.Symbol] -= t.Quantity
		} else {
			net[t.Symbol] += t.Quantity
		}
	}
	var out []string
	for s, n := range net {
		if n > 1e-9 {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
