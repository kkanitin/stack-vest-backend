package sentimentuc

import (
	"context"
	"time"

	sentimentdomain "github.com/kanitin/stackvest/backend/internal/domain/sentiment"
	"github.com/kanitin/stackvest/backend/internal/domain/stock"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
	"github.com/kanitin/stackvest/backend/pkg/cache"
)

const (
	vixSymbol   = "^VIX"
	indexSymbol = "^GSPC" // S&P 500 — momentum proxy
)

// marketDataProvider is implemented by *fmp.Client.
type marketDataProvider interface {
	GetQuote(symbol string) (*stock.Quote, error)
	GetBiggestGainers() ([]fmp.MarketMover, error)
	GetBiggestLosers() ([]fmp.MarketMover, error)
}

// UseCase computes the daily composite sentiment score from FMP-sourced
// signals (VIX level, index momentum, market breadth), cached for ttl so the
// underlying FMP calls happen at most once per cache window.
type UseCase struct {
	market marketDataProvider
	cache  *cache.TTL[*sentimentdomain.Score]
}

func NewUseCase(market marketDataProvider, ttl time.Duration) *UseCase {
	return &UseCase{market: market, cache: cache.NewTTL[*sentimentdomain.Score](ttl)}
}

func (uc *UseCase) Execute(ctx context.Context) (*sentimentdomain.Score, error) {
	if cached, ok := uc.cache.Get(); ok {
		return cached, nil
	}

	vix, err := uc.market.GetQuote(vixSymbol)
	if err != nil {
		return nil, err
	}
	index, err := uc.market.GetQuote(indexSymbol)
	if err != nil {
		return nil, err
	}
	gainers, err := uc.market.GetBiggestGainers()
	if err != nil {
		return nil, err
	}
	losers, err := uc.market.GetBiggestLosers()
	if err != nil {
		return nil, err
	}

	vixScore := sentimentdomain.ScoreFromVIX(vix.Price)
	momentumScore := sentimentdomain.ScoreFromMomentum(index.ChangePercent)
	breadthScore := sentimentdomain.ScoreFromBreadth(len(gainers), len(losers))
	composite := sentimentdomain.CompositeScore(vixScore, momentumScore, breadthScore)

	result := &sentimentdomain.Score{
		Score:  composite,
		Status: sentimentdomain.StatusFromScore(composite),
		Signals: sentimentdomain.Signals{
			VIX:                vix.Price,
			IndexChangePercent: index.ChangePercent,
			GainersCount:       len(gainers),
			LosersCount:        len(losers),
		},
		Timestamp: time.Now().UTC(),
	}
	uc.cache.Set(result)
	return result, nil
}
