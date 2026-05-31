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
	repo         portfoliodomain.Repository
	userRepo     userdomain.Repository
	quoter       stockdomain.Quoter
	priceChanger stockdomain.PriceChanger
}

func New(
	repo portfoliodomain.Repository,
	userRepo userdomain.Repository,
	quoter stockdomain.Quoter,
	priceChanger stockdomain.PriceChanger,
) *UseCase {
	return &UseCase{repo: repo, userRepo: userRepo, quoter: quoter, priceChanger: priceChanger}
}

func (uc *UseCase) AddPosition(ctx context.Context, email, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.Add(ctx, user.ID, symbol, name, shares, avgCost)
}

func (uc *UseCase) RemovePosition(ctx context.Context, email, symbol string) error {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.Remove(ctx, user.ID, symbol)
}

func (uc *UseCase) UpdatePosition(ctx context.Context, email, symbol string, shares, avgCost *float64) (*portfoliodomain.Position, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.Update(ctx, user.ID, symbol, shares, avgCost)
}

func (uc *UseCase) ListPositions(ctx context.Context, email string) ([]*portfoliodomain.Position, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	positions, err := uc.repo.ListByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	uc.enrichPositions(ctx, positions)
	return positions, nil
}

func (uc *UseCase) GetSummary(ctx context.Context, email string) (*portfoliodomain.Summary, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	positions, err := uc.repo.ListByUserID(ctx, user.ID)
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

func (uc *UseCase) GetActivity(ctx context.Context, email string, limit int) ([]*portfoliodomain.Activity, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user lookup: %w", err)
	}
	return uc.repo.GetActivity(ctx, user.ID, limit)
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
