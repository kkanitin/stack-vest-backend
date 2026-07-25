package sentimentuc

import (
	"context"
	"fmt"
	"sync"
	"time"

	sentimentdomain "github.com/kanitin/stackvest/backend/internal/domain/sentiment"
	"github.com/kanitin/stackvest/backend/internal/domain/stock"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

const (
	vixSymbol   = "^VIX"
	indexSymbol = "^GSPC" // S&P 500 — momentum proxy

	// negativeTTL: a failed compute is retried after this interval rather than
	// on every request during an outage.
	negativeTTL = time.Minute
)

// marketDataProvider is implemented by *fmp.Client.
type marketDataProvider interface {
	GetQuote(symbol string) (*stock.Quote, error)
	GetBiggestGainers() ([]fmp.MarketMover, error)
	GetBiggestLosers() ([]fmp.MarketMover, error)
}

// UseCase computes the daily composite sentiment score from FMP-sourced
// signals (VIX level, index momentum, market breadth), cached for ttl so the
// underlying FMP calls happen at most once per cache window. Concurrent
// misses are coalesced via TTL.Fill's singleflight.
type UseCase struct {
	market marketDataProvider
	cache  *cache.TTL[*sentimentdomain.Score]
}

func NewUseCase(market marketDataProvider, ttl time.Duration) *UseCase {
	return &UseCase{market: market, cache: cache.NewTTLWithNegative[*sentimentdomain.Score](ttl, negativeTTL)}
}

func (uc *UseCase) Execute(ctx context.Context) (*sentimentdomain.Score, error) {
	result, healthy := uc.cache.Fill(func() (*sentimentdomain.Score, bool) {
		score, err := uc.compute(ctx)
		if err != nil {
			return nil, false
		}
		return score, true
	})
	if !healthy {
		return nil, fmt.Errorf("sentiment data unavailable")
	}
	return result, nil
}

// compute fetches the four independent signals in parallel — none depends on
// another's result — then derives the composite score.
func (uc *UseCase) compute(ctx context.Context) (*sentimentdomain.Score, error) {
	var (
		vix, index            *stock.Quote
		gainers, losers       []fmp.MarketMover
		vixErr, indexErr      error
		gainersErr, losersErr error
		wg                    sync.WaitGroup
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		vix, vixErr = uc.market.GetQuote(vixSymbol)
	}()
	go func() {
		defer wg.Done()
		index, indexErr = uc.market.GetQuote(indexSymbol)
	}()
	go func() {
		defer wg.Done()
		gainers, gainersErr = uc.market.GetBiggestGainers()
	}()
	go func() {
		defer wg.Done()
		losers, losersErr = uc.market.GetBiggestLosers()
	}()
	wg.Wait()

	if vixErr != nil {
		return nil, vixErr
	}
	if indexErr != nil {
		return nil, indexErr
	}
	if gainersErr != nil {
		return nil, gainersErr
	}
	if losersErr != nil {
		return nil, losersErr
	}

	vixScore := sentimentdomain.ScoreFromVIX(vix.Price)
	momentumScore := sentimentdomain.ScoreFromMomentum(index.ChangePercent)
	breadthScore := sentimentdomain.ScoreFromBreadth(len(gainers), len(losers))
	composite := sentimentdomain.CompositeScore(vixScore, momentumScore, breadthScore)

	return &sentimentdomain.Score{
		Score:  composite,
		Status: sentimentdomain.StatusFromScore(composite),
		Signals: sentimentdomain.Signals{
			VIX:                vix.Price,
			IndexChangePercent: index.ChangePercent,
			GainersCount:       len(gainers),
			LosersCount:        len(losers),
		},
		Timestamp: time.Now().UTC(),
	}, nil
}
