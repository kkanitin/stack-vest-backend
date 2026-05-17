package watchlist

import (
	"context"
	"fmt"
	"strings"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	watchlistdomain "github.com/kanitin/stackvest/backend/internal/domain/watchlist"
)

type WatchlistUseCase struct {
	repo          watchlistdomain.Repository
	userRepo      userdomain.Repository
	stockSearcher stockdomain.Searcher
}

func NewWatchlistUseCase(
	repo watchlistdomain.Repository,
	userRepo userdomain.Repository,
	stockSearcher stockdomain.Searcher,
) *WatchlistUseCase {
	return &WatchlistUseCase{repo: repo, userRepo: userRepo, stockSearcher: stockSearcher}
}

func (uc *WatchlistUseCase) Add(ctx context.Context, email, symbol, name, itemType string) (
	*watchlistdomain.Item, error,
) {
	matches, err := uc.stockSearcher.SearchSymbol(symbol)
	if err != nil {
		return nil, fmt.Errorf("symbol validation: %w", err)
	}
	if len(matches) == 0 {
		return nil, watchlistdomain.ErrInvalidSymbol
	}
	validated := matches[0]
	for _, m := range matches {
		if strings.EqualFold(m.Symbol, symbol) {
			validated = m
			break
		}
	}
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return uc.repo.Add(
		ctx, &watchlistdomain.Item{
			UserID: user.ID,
			Symbol: validated.Symbol,
			Name:   validated.Name,
			Type:   validated.Type,
		},
	)
}

func (uc *WatchlistUseCase) Remove(ctx context.Context, email, symbol string) error {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	return uc.repo.Remove(ctx, user.ID, symbol)
}

func (uc *WatchlistUseCase) List(ctx context.Context, email string) ([]watchlistdomain.Item, int, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, 0, err
	}
	return uc.repo.ListByUserID(ctx, user.ID)
}
