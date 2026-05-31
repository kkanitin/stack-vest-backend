package stockuc

import (
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type HistoryUseCase struct {
	fetcher domain.HistoryFetcher
}

func NewHistoryUseCase(f domain.HistoryFetcher) *HistoryUseCase {
	return &HistoryUseCase{fetcher: f}
}

func (uc *HistoryUseCase) Execute(symbol string, r domain.HistoryRange) (*domain.History, error) {
	to := time.Now().UTC().Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -r.Days())

	points, err := uc.fetcher.GetHistoryClose(symbol, from, to)
	if err != nil {
		return nil, err
	}
	return &domain.History{
		Symbol: symbol,
		Range:  r,
		Points: points,
	}, nil
}
