package portfolio

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

// benchmarkLookback is how far before the first portfolio day closes are requested, so
// that a first day falling on a weekend or holiday still has an earlier close to use.
const benchmarkLookback = 7 * 24 * time.Hour

// WithBenchmarks enables the benchmark overlay: list is what GET /portfolios/benchmarks
// serves and what GetValueHistory accepts, and fetcher supplies the index closes. A nil
// fetcher is allowed; the overlay then reports itself unavailable. It is a setter, not a
// New parameter, so the many New call sites stay unchanged. Call it once at wiring time.
func (uc *UseCase) WithBenchmarks(fetcher stockdomain.HistoryFetcher, list []portfoliodomain.Benchmark) *UseCase {
	uc.benchmarkFetcher = fetcher
	uc.benchmarks = list
	return uc
}

// Benchmarks returns the configured benchmarks in config order.
func (uc *UseCase) Benchmarks() []portfoliodomain.Benchmark {
	out := make([]portfoliodomain.Benchmark, len(uc.benchmarks))
	copy(out, uc.benchmarks)
	return out
}

func (uc *UseCase) findBenchmark(symbol string) *portfoliodomain.Benchmark {
	for i := range uc.benchmarks {
		if uc.benchmarks[i].Symbol == symbol {
			return &uc.benchmarks[i]
		}
	}
	return nil
}

// attachBenchmark fills BenchmarkClose on points and returns the info for the response.
// It never fails the request: with fewer than two points there is nothing to compare, so
// the provider is not called and the benchmark is reported available with no closes; if
// the fetcher is nil or errors, the benchmark is reported unavailable and a warning is
// logged.
func (uc *UseCase) attachBenchmark(ctx context.Context, b portfoliodomain.Benchmark, points []*portfoliodomain.ValuePoint) *portfoliodomain.BenchmarkInfo {
	info := &portfoliodomain.BenchmarkInfo{Symbol: b.Symbol, Label: b.Label, Available: true}
	if len(points) < 2 {
		return info
	}

	closes, err := uc.fetchBenchmarkCloses(b.Symbol, points[0].Date, points[len(points)-1].Date)
	if err != nil {
		zap.L().Warn(
			"benchmark closes unavailable", logger.RequestID(ctx), zap.String("symbol", b.Symbol), zap.Error(err),
		)
		info.Available = false
		return info
	}
	alignBenchmark(points, closes)
	return info
}

func (uc *UseCase) fetchBenchmarkCloses(symbol, firstDay, lastDay string) ([]stockdomain.HistoryPoint, error) {
	if uc.benchmarkFetcher == nil {
		return nil, errNoBenchmarkFetcher
	}
	first, err := time.Parse("2006-01-02", firstDay)
	if err != nil {
		return nil, err
	}
	last, err := time.Parse("2006-01-02", lastDay)
	if err != nil {
		return nil, err
	}
	return uc.benchmarkFetcher.GetHistoryClose(symbol, first.Add(-benchmarkLookback), last)
}

// alignBenchmark sets BenchmarkClose on each point to the close on its date, or the
// latest close before it (forward fill over weekends and holidays). A point with no close
// on or before its date, for instance when closes is empty, is left nil. Both slices must
// be sorted oldest first; one pass walks them together. Dates are YYYY-MM-DD, so string
// comparison is chronological.
func alignBenchmark(points []*portfoliodomain.ValuePoint, closes []stockdomain.HistoryPoint) {
	i := 0
	var last *float64
	for _, p := range points {
		for i < len(closes) && closes[i].Date <= p.Date {
			c := closes[i].Close
			last = &c
			i++
		}
		if last != nil {
			v := *last
			p.BenchmarkClose = &v
		}
	}
}

var errNoBenchmarkFetcher = errors.New("no benchmark fetcher configured")
