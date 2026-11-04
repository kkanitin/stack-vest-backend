package portfolio

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
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
	monthly := map[string]map[int]float64{}
	for _, p := range priced {
		r := returns[p.Symbol]
		if r.skip != nil {
			out.Skipped = append(out.Skipped, *r.skip)
			continue
		}
		value := p.Shares * prices[p.Symbol]
		totalValue += value
		monthly[p.Symbol] = r.monthly
		out.Holdings = append(out.Holdings, portfoliodomain.SuggestedReturnHolding{
			Symbol:           p.Symbol,
			Name:             p.Name,
			ValueUsd:         value,
			AnnualReturn:     r.cagr,
			AnnualVolatility: annualVolatility(logValues(r.monthly)),
			StartDate:        r.first.Format(portfoliodomain.DateLayout),
			EndDate:          r.last.Format(portfoliodomain.DateLayout),
			Years:            round2(r.years),
			ShortHistory:     r.first.After(start.Add(fullHistorySlack)),
		})
	}
	sort.SliceStable(out.Skipped, func(i, j int) bool { return out.Skipped[i].Symbol < out.Skipped[j].Symbol })

	if len(out.Holdings) == 0 || totalValue <= 0 {
		out.Holdings = []portfoliodomain.SuggestedReturnHolding{}
		out.Note = "None of this portfolio's holdings has a current price and enough price history to suggest a return."
		return out, nil
	}

	weights := make(map[string]float64, len(out.Holdings))
	for _, h := range out.Holdings {
		weights[h.Symbol] = h.ValueUsd / totalValue
	}
	vol := annualVolatility(portfolioMonthlyLogReturns(weights, monthly))

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
	out.AnnualVolatility = vol
	var notes []string
	if lookback.ShortHistory {
		notes = append(notes, "Some holdings have less than 10 years of price history; their returns use all the history available.")
	}
	if vol == nil {
		notes = append(notes, "There is not enough shared monthly price history (at least 2 years) to estimate a volatility.")
	}
	out.Note = strings.Join(notes, " ")
	return out, nil
}

// holdingReturn is one symbol's CAGR over its closes in the window, or why it was skipped.
type holdingReturn struct {
	cagr        float64 // percent
	years       float64
	first, last time.Time
	// monthly holds the log return of each calendar month, keyed by monthKey of the month
	// it ends in.
	monthly map[int]float64
	skip    *portfoliodomain.SkippedHolding
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
	r.monthly = monthlyLogReturns(closes)
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

// monthKey numbers calendar months consecutively, so adjacent months differ by one.
func monthKey(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

// monthlyLogReturns takes the last positive close of each calendar month and returns
// ln(close / previous month's close) for each month whose previous month also has a close,
// keyed by monthKey. The first month (often partial) only serves as the starting point.
func monthlyLogReturns(closes []dcadomain.HistoricalPrice) map[int]float64 {
	type monthEnd struct {
		date  time.Time
		close float64
	}
	ends := map[int]monthEnd{}
	for _, c := range closes {
		if c.Close <= 0 {
			continue
		}
		k := monthKey(c.Date)
		if e, ok := ends[k]; !ok || c.Date.After(e.date) {
			ends[k] = monthEnd{date: c.Date, close: c.Close}
		}
	}
	out := make(map[int]float64, len(ends))
	for k, e := range ends {
		if prev, ok := ends[k-1]; ok {
			out[k] = math.Log(e.close / prev.close)
		}
	}
	return out
}

// portfolioMonthlyLogReturns combines the holdings into one fixed-weight portfolio: for
// each month every holding has a return for, the weighted sum of the holdings' simple
// returns, as a log return. Weighting returns rather than volatilities keeps the
// diversification between holdings that are not perfectly correlated.
func portfolioMonthlyLogReturns(weights map[string]float64, monthly map[string]map[int]float64) []float64 {
	var shared []int
	first := true
	for sym := range weights {
		if first {
			for k := range monthly[sym] {
				shared = append(shared, k)
			}
			first = false
			continue
		}
		kept := shared[:0]
		for _, k := range shared {
			if _, ok := monthly[sym][k]; ok {
				kept = append(kept, k)
			}
		}
		shared = kept
	}
	sort.Ints(shared)
	out := make([]float64, 0, len(shared))
	for _, k := range shared {
		var r float64
		for sym, w := range weights {
			r += w * math.Expm1(monthly[sym][k])
		}
		if r <= -1 {
			continue // a total loss has no log return
		}
		out = append(out, math.Log1p(r))
	}
	return out
}

func logValues(m map[int]float64) []float64 {
	out := make([]float64, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// annualVolatility is the sample standard deviation of monthly log returns × √12, in
// percent rounded to 2 dp. Nil with fewer than MinVolatilityMonths returns.
func annualVolatility(logReturns []float64) *float64 {
	n := len(logReturns)
	if n < portfoliodomain.MinVolatilityMonths {
		return nil
	}
	var mean float64
	for _, r := range logReturns {
		mean += r
	}
	mean /= float64(n)
	var ss float64
	for _, r := range logReturns {
		ss += (r - mean) * (r - mean)
	}
	v := round2(math.Sqrt(ss/float64(n-1)) * math.Sqrt(12) * 100)
	return &v
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
