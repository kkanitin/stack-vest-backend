package portfolio

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type UseCase struct {
	repo          portfoliodomain.Repository
	userRepo      userdomain.Repository
	quoter        stockdomain.Quoter
	priceChanger  stockdomain.PriceChanger
	maxPortfolios int
	maxPositions  int
}

func New(
	repo portfoliodomain.Repository,
	userRepo userdomain.Repository,
	quoter stockdomain.Quoter,
	priceChanger stockdomain.PriceChanger,
	maxPortfolios int,
	maxPositions int,
) *UseCase {
	return &UseCase{
		repo:          repo,
		userRepo:      userRepo,
		quoter:        quoter,
		priceChanger:  priceChanger,
		maxPortfolios: maxPortfolios,
		maxPositions:  maxPositions,
	}
}

// ownedPortfolio verifies that the portfolio identified by portfolioID belongs to
// the authenticated user. A portfolio that does not exist or is owned by someone
// else both surface as ErrPortfolioNotFound (404) so existence is not leaked.
func (uc *UseCase) ownedPortfolio(ctx context.Context, email, portfolioID string) (*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	p, err := uc.repo.GetPortfolio(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if p.UserID != user.ID {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	return p, nil
}

// --- Portfolios ---

func (uc *UseCase) CreatePortfolio(ctx context.Context, email, name, description string) (*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	count, err := uc.repo.CountPortfolios(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if count >= uc.maxPortfolios {
		return nil, portfoliodomain.ErrPortfolioLimitReached
	}
	return uc.repo.CreatePortfolio(ctx, user.ID, name, description)
}

func (uc *UseCase) ListPortfolios(ctx context.Context, email string) ([]*portfoliodomain.Portfolio, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.ListPortfolios(ctx, user.ID)
}

func (uc *UseCase) GetPortfolio(ctx context.Context, email, portfolioID string) (*portfoliodomain.Portfolio, error) {
	return uc.ownedPortfolio(ctx, email, portfolioID)
}

func (uc *UseCase) UpdatePortfolio(ctx context.Context, email, portfolioID string, name, description *string) (*portfoliodomain.Portfolio, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.UpdatePortfolio(ctx, portfolioID, name, description)
}

func (uc *UseCase) DeletePortfolio(ctx context.Context, email, portfolioID string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.DeletePortfolio(ctx, portfolioID)
}

// --- Positions ---

func (uc *UseCase) AddPosition(ctx context.Context, email, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	count, err := uc.repo.CountPositions(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if count >= uc.maxPositions {
		return nil, portfoliodomain.ErrPositionLimitReached
	}
	return uc.repo.Add(ctx, portfolioID, symbol, name, shares, avgCost)
}

func (uc *UseCase) RemovePosition(ctx context.Context, email, portfolioID, symbol string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.Remove(ctx, portfolioID, symbol)
}

func (uc *UseCase) UpdatePosition(ctx context.Context, email, portfolioID, symbol string, shares, avgCost *float64) (*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.Update(ctx, portfolioID, symbol, shares, avgCost)
}

func (uc *UseCase) ListPositions(ctx context.Context, email, portfolioID string) ([]*portfoliodomain.Position, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	uc.enrichPositions(ctx, positions)
	return positions, nil
}

func (uc *UseCase) GetSummary(ctx context.Context, email, portfolioID string) (*portfoliodomain.Summary, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	positions, err := uc.repo.ListByPortfolioID(ctx, portfolioID)
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return &portfoliodomain.Summary{}, nil
	}

	type priceData struct {
		currentPrice float64
		change1M     float64
		ok           bool
	}
	data := make([]priceData, len(positions))

	g := new(errgroup.Group)
	for i, pos := range positions {
		i, pos := i, pos
		g.Go(func() error {
			q, err := uc.quoter.GetQuote(pos.Symbol)
			if err != nil {
				slog.WarnContext(ctx, "failed to get quote for summary", "symbol", pos.Symbol, "error", err)
				return nil
			}
			pc, err := uc.priceChanger.GetPriceChange(pos.Symbol)
			if err != nil {
				slog.WarnContext(ctx, "failed to get price change for summary", "symbol", pos.Symbol, "error", err)
				return nil
			}
			data[i] = priceData{currentPrice: q.Price, change1M: pc.M1, ok: true}
			return nil
		})
	}
	g.Wait()

	var totalValue, totalValue30dAgo float64
	for i, pos := range positions {
		if !data[i].ok {
			continue
		}
		valueUsd := pos.Shares * data[i].currentPrice
		divisor := 1 + data[i].change1M/100
		if divisor == 0 {
			continue
		}
		totalValue += valueUsd
		totalValue30dAgo += valueUsd / divisor
	}

	change30d := totalValue - totalValue30dAgo
	var changePct30d float64
	if totalValue30dAgo != 0 {
		changePct30d = change30d / totalValue30dAgo * 100
	}
	return &portfoliodomain.Summary{
		TotalValue:   totalValue,
		Change30d:    change30d,
		ChangePct30d: changePct30d,
	}, nil
}

func (uc *UseCase) GetActivity(ctx context.Context, email, portfolioID string, limit int) ([]*portfoliodomain.Activity, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	return uc.repo.GetActivity(ctx, portfolioID, limit)
}

func (uc *UseCase) enrichPositions(ctx context.Context, positions []*portfoliodomain.Position) {
	g := new(errgroup.Group)
	for _, pos := range positions {
		pos := pos
		g.Go(func() error {
			q, err := uc.quoter.GetQuote(pos.Symbol)
			if err != nil {
				slog.WarnContext(ctx, "failed to get quote", "symbol", pos.Symbol, "error", err)
				return nil
			}
			pos.ValueUsd = pos.Shares * q.Price
			pos.Change24h = q.ChangePercent
			return nil
		})
	}
	g.Wait()
}
