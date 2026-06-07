package stockuc

import (
	"context"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"golang.org/x/sync/errgroup"
)

type DetailUseCase struct {
	fetcher domain.DetailFetcher
}

func NewDetailUseCase(f domain.DetailFetcher) *DetailUseCase {
	return &DetailUseCase{fetcher: f}
}

func (uc *DetailUseCase) Execute(ctx context.Context, symbol string, r domain.DetailRange) (*domain.AssetDetail, error) {
	var profile *domain.AssetProfile
	var points []domain.DetailPoint

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		p, err := uc.fetcher.GetProfile(symbol)
		if err != nil {
			return err
		}
		profile = p
		return nil
	})
	g.Go(func() error {
		if r.IsIntraday() {
			pts, err := uc.fetcher.GetIntradayOHLCV(symbol)
			if err != nil {
				return err
			}
			points = pts
			return nil
		}
		to := time.Now().UTC().Truncate(24 * time.Hour)
		from := to.AddDate(0, 0, -r.Days())
		pts, err := uc.fetcher.GetDailyOHLCV(symbol, from, to)
		if err != nil {
			return err
		}
		points = pts
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	interval := "daily"
	if r.IsIntraday() {
		interval = "intraday"
	}

	return &domain.AssetDetail{
		Symbol:   symbol,
		Name:     profile.Name,
		Currency: profile.Currency,
		Range:    r,
		Interval: interval,
		Points:   points,
	}, nil
}
