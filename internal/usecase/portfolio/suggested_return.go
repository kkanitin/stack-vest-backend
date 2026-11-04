package portfolio

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	dcadomain "github.com/kanitin/stackvest/backend/internal/domain/dca"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

const (
	// suggestedReturnConcurrency bounds the long price-history fetches per request, as the
	// DCA holdings simulation does.
	suggestedReturnConcurrency = 4
	// minSuggestedReturnYears is the shortest history a holding needs: annualising a few
	// weeks of movement gives wild figures, so anything shorter is skipped as no history.
	minSuggestedReturnYears = 1.0
	// fullHistorySlack is how late the first close may be and still count as the full
	// lookback (the lookback start can fall on a weekend or holiday).
	fullHistorySlack = 7 * 24 * time.Hour
	daysPerYear      = 365.25

	suggestedReturnPriceBasisNote = "Price movement only: dividends are not included, the same basis as the DCA simulator."
)

// WithLongHistory sets the daily-close source for SuggestedReturn. It needs up to
// SuggestedReturnLookbackYears of history, which the five-year HistoryCloser cannot serve,
// so it takes the DCA price source (plain closes: split-adjusted, no dividends). A nil
// fetcher is allowed; every holding is then skipped as history unavailable.
func (uc *UseCase) WithLongHistory(fetcher dcadomain.PriceFetcher) *UseCase {
	uc.longHistory = fetcher
	return uc
}

// SuggestedReturn suggests an expected annual return for an owned portfolio: for each open
// holding, the CAGR of its daily closes over the last ten years (or all available history
// when shorter), combined by current value weight. Holdings without a live price or
// without usable history are listed in Skipped. With nothing usable it returns a result
// with Available false rather than an error. ErrPortfolioNotFound (404) when the portfolio
// is missing or not owned.
func (uc *UseCase) SuggestedReturn(ctx context.Context, email, portfolioID string, now time.Time) (*portfoliodomain.SuggestedReturn, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID, false)
	if err != nil {
		return nil, err
	}
	open := make([]*portfoliodomain.Position, 0, len(positions))
	for _, p := range positions {
		if p.Shares > 0 {
			open = append(open, p)
		}
	}

	out := &portfoliodomain.SuggestedReturn{
		Holdings:       []portfoliodomain.SuggestedReturnHolding{},
		Skipped:        []portfoliodomain.SkippedHolding{},
		PriceBasisNote: suggestedReturnPriceBasisNote,
	}
	if len(open) == 0 {
		out.Note = "This portfolio has no open holdings to base a return on."
		return out, nil
	}

	prices := map[string]float64{}
	if uc.quoter != nil {
		prices = uc.fetchQuotePrices(ctx, distinctSymbols(open))
	}

	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(-portfoliodomain.SuggestedReturnLookbackYears, 0, 0)

	var priced []*portfoliodomain.Position
	for _, p := range open {
		if _, ok := prices[p.Symbol]; !ok {
			out.Skipped = append(out.Skipped, portfoliodomain.SkippedHolding{
				Symbol: p.Symbol, Code: portfoliodomain.SkipNoPrice, Reason: "No current price is available.",
			})
			continue
		}
		priced = append(priced, p)
	}

	returns := uc.holdingReturns(ctx, priced, start, end)

	var totalValue float64
	for _, p := range priced {
		r := returns[p.Symbol]
		if r.skip != nil {
			out.Skipped = append(out.Skipped, *r.skip)
			continue
		}
		value := p.Shares * prices[p.Symbol]
		totalValue += value
		out.Holdings = append(out.Holdings, portfoliodomain.SuggestedReturnHolding{
			Symbol:       p.Symbol,
			Name:         p.Name,
			ValueUsd:     value,
			AnnualReturn: r.cagr,
			StartDate:    r.first.Format(portfoliodomain.DateLayout),
			EndDate:      r.last.Format(portfoliodomain.DateLayout),
			Years:        round2(r.years),
			ShortHistory: r.first.After(start.Add(fullHistorySlack)),
		})
	}
	sort.SliceStable(out.Skipped, func(i, j int) bool { return out.Skipped[i].Symbol < out.Skipped[j].Symbol })

	if len(out.Holdings) == 0 || totalValue <= 0 {
		out.Holdings = []portfoliodomain.SuggestedReturnHolding{}
		out.Note = "None of this portfolio's holdings has a current price and enough price history to suggest a return."
		return out, nil
	}

	var annual float64
	lookback := &portfoliodomain.ReturnLookback{RequestedYears: portfoliodomain.SuggestedReturnLookbackYears}
	for i := range out.Holdings {
		h := &out.Holdings[i]
		w := h.ValueUsd / totalValue
		annual += w * h.AnnualReturn
		h.WeightPct = round2(w * 100)
		h.AnnualReturn = round2(h.AnnualReturn)
		if lookback.StartDate == "" || h.StartDate < lookback.StartDate {
			lookback.StartDate = h.StartDate
		}
		if h.EndDate > lookback.EndDate {
			lookback.EndDate = h.EndDate
		}
		lookback.ShortHistory = lookback.ShortHistory || h.ShortHistory
	}
	lookback.Years = round2(yearsBetweenDates(lookback.StartDate, lookback.EndDate))
	sort.SliceStable(out.Holdings, func(i, j int) bool {
		if out.Holdings[i].ValueUsd != out.Holdings[j].ValueUsd {
			return out.Holdings[i].ValueUsd > out.Holdings[j].ValueUsd
		}
		return out.Holdings[i].Symbol < out.Holdings[j].Symbol
	})

	annual = round2(annual)
	out.Available = true
	out.AnnualReturn = &annual
	out.Lookback = lookback
	if lookback.ShortHistory {
		out.Note = "Some holdings have less than 10 years of price history; their returns use all the history available."
	}
	return out, nil
}

// holdingReturn is one symbol's CAGR over its closes in the window, or why it was skipped.
type holdingReturn struct {
	cagr        float64 // percent
	years       float64
	first, last time.Time
	skip        *portfoliodomain.SkippedHolding
}

// holdingReturns fetches each position's closes in [start, end] (at most
// suggestedReturnConcurrency at once) and computes its CAGR. Failures never fail the
// request: they become skips.
func (uc *UseCase) holdingReturns(ctx context.Context, positions []*portfoliodomain.Position, start, end time.Time) map[string]holdingReturn {
	out := make(map[string]holdingReturn, len(positions))
	var mu sync.Mutex
	g := new(errgroup.Group)
	g.SetLimit(suggestedReturnConcurrency)
	for _, p := range positions {
		sym := p.Symbol
		g.Go(func() error {
			r := uc.holdingReturn(ctx, sym, start, end)
			mu.Lock()
			out[sym] = r
			mu.Unlock()
			return nil
		})
	}
	g.Wait()
	return out
}

func (uc *UseCase) holdingReturn(ctx context.Context, symbol string, start, end time.Time) holdingReturn {
	skip := func(code, reason string) holdingReturn {
		return holdingReturn{skip: &portfoliodomain.SkippedHolding{Symbol: symbol, Code: code, Reason: reason}}
	}
	if uc.longHistory == nil {
		return skip(portfoliodomain.SkipHistoryUnavailable, "Price history is not available right now.")
	}
	closes, err := uc.longHistory.GetHistoricalPrices(symbol, start, end)
	if errors.Is(err, dcadomain.ErrSymbolNotFound) {
		return skip(portfoliodomain.SkipNoHistory, "No price history is available.")
	}
	if err != nil {
		zap.L().Warn("suggested return: failed to get price history", logger.RequestID(ctx), zap.String("symbol", symbol), zap.Error(err))
		return skip(portfoliodomain.SkipHistoryUnavailable, "Price history is not available right now.")
	}
	r, ok := cagr(closes)
	if !ok {
		return skip(portfoliodomain.SkipNoHistory, "No price history is available.")
	}
	if r.years < minSuggestedReturnYears {
		return skip(portfoliodomain.SkipNoHistory, "Less than one year of price history is available.")
	}
	return r
}

// cagr is the compound annual growth rate (percent) from the first to the last positive
// close, on a 365.25-day year. Closes may be in any order. ok is false with fewer than two
// usable closes or a zero-length span.
func cagr(closes []dcadomain.HistoricalPrice) (holdingReturn, bool) {
	var first, last *dcadomain.HistoricalPrice
	for i := range closes {
		c := &closes[i]
		if c.Close <= 0 {
			continue
		}
		if first == nil || c.Date.Before(first.Date) {
			first = c
		}
		if last == nil || c.Date.After(last.Date) {
			last = c
		}
	}
	if first == nil || !last.Date.After(first.Date) {
		return holdingReturn{}, false
	}
	years := last.Date.Sub(first.Date).Hours() / 24 / daysPerYear
	rate := (math.Pow(last.Close/first.Close, 1/years) - 1) * 100
	return holdingReturn{cagr: rate, years: years, first: first.Date, last: last.Date}, true
}

func yearsBetweenDates(from, to string) float64 {
	f, err1 := time.Parse(portfoliodomain.DateLayout, from)
	t, err2 := time.Parse(portfoliodomain.DateLayout, to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return t.Sub(f).Hours() / 24 / daysPerYear
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
