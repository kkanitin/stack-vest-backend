package stockuc

import (
	"context"
	"errors"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"golang.org/x/sync/errgroup"
)

type BatchHistoryUseCase struct {
	fetcher domain.HistoryFetcher
}

func NewBatchHistoryUseCase(f domain.HistoryFetcher) *BatchHistoryUseCase {
	return &BatchHistoryUseCase{fetcher: f}
}

func (uc *BatchHistoryUseCase) Execute(ctx context.Context, symbolsParam string, r domain.BatchHistoryRange) ([]domain.BatchHistoryItem, error) {
	syms, err := parseSymbols(symbolsParam)
	if err != nil {
		return nil, err
	}

	to := time.Now().UTC().Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -r.Days())

	g, _ := errgroup.WithContext(ctx)
	results := make([]domain.BatchHistoryItem, len(syms))
	for i, sym := range syms {
		g.Go(func() error {
			pts, err := uc.fetcher.GetHistoryClose(sym, from, to)
			if errors.Is(err, domain.ErrSymbolNotFound) {
				results[i] = domain.BatchHistoryItem{Symbol: sym, Range: string(r), Points: []domain.HistoryPoint{}}
				return nil
			}
			if err != nil {
				return err
			}
			results[i] = domain.BatchHistoryItem{Symbol: sym, Range: string(r), Points: pts}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}
