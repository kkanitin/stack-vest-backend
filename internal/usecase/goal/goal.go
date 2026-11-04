package goal

import (
	"context"
	"errors"
	"fmt"

	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

// PortfolioGetter is the slice of the portfolio repository the goal use case needs to
// check that a linked portfolio belongs to the user.
type PortfolioGetter interface {
	GetPortfolio(ctx context.Context, id string) (*portfoliodomain.Portfolio, error)
}

type UseCase struct {
	repo       goaldomain.Repository
	userRepo   userdomain.Repository
	portfolios PortfolioGetter
	maxPerUser int
}

func New(repo goaldomain.Repository, userRepo userdomain.Repository, portfolios PortfolioGetter, maxPerUser int) *UseCase {
	return &UseCase{repo: repo, userRepo: userRepo, portfolios: portfolios, maxPerUser: maxPerUser}
}

func (uc *UseCase) userID(ctx context.Context, email string) (string, error) {
	user, err := uc.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("user lookup: %w", err)
	}
	return user.ID, nil
}

// ownedGoal loads goalID and checks it belongs to userID. A missing goal and someone
// else's goal both return ErrGoalNotFound so existence is not leaked.
func (uc *UseCase) ownedGoal(ctx context.Context, userID, goalID string) (*goaldomain.Goal, error) {
	g, err := uc.repo.Get(ctx, goalID)
	if err != nil {
		return nil, err
	}
	if g.UserID != userID {
		return nil, goaldomain.ErrGoalNotFound
	}
	return g, nil
}

// checkPortfolio returns ErrInvalidPortfolio unless portfolioID is nil or one of userID's portfolios.
func (uc *UseCase) checkPortfolio(ctx context.Context, userID string, portfolioID *string) error {
	if portfolioID == nil {
		return nil
	}
	p, err := uc.portfolios.GetPortfolio(ctx, *portfolioID)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		return goaldomain.ErrInvalidPortfolio
	}
	if err != nil {
		return fmt.Errorf("portfolio lookup: %w", err)
	}
	if p.UserID != userID {
		return goaldomain.ErrInvalidPortfolio
	}
	return nil
}

// normalize fills the defaults for optional inputs.
func normalize(g *goaldomain.Goal) {
	if g.Currency == "" {
		g.Currency = goaldomain.DefaultCurrency
	}
}

func (uc *UseCase) Create(ctx context.Context, email string, g *goaldomain.Goal) (*goaldomain.Goal, error) {
	userID, err := uc.userID(ctx, email)
	if err != nil {
		return nil, err
	}
	if err := uc.checkPortfolio(ctx, userID, g.PortfolioID); err != nil {
		return nil, err
	}
	g.UserID = userID
	normalize(g)
	return uc.repo.Create(ctx, g, uc.maxPerUser)
}

func (uc *UseCase) List(ctx context.Context, email string, limit, offset int) ([]*goaldomain.Goal, int, error) {
	userID, err := uc.userID(ctx, email)
	if err != nil {
		return nil, 0, err
	}
	return uc.repo.ListByUser(ctx, userID, limit, offset)
}

func (uc *UseCase) Get(ctx context.Context, email, goalID string) (*goaldomain.Goal, error) {
	userID, err := uc.userID(ctx, email)
	if err != nil {
		return nil, err
	}
	return uc.ownedGoal(ctx, userID, goalID)
}

// Update replaces every input of the goal; a nil PortfolioID unlinks it.
func (uc *UseCase) Update(ctx context.Context, email, goalID string, g *goaldomain.Goal) (*goaldomain.Goal, error) {
	userID, err := uc.userID(ctx, email)
	if err != nil {
		return nil, err
	}
	if _, err := uc.ownedGoal(ctx, userID, goalID); err != nil {
		return nil, err
	}
	if err := uc.checkPortfolio(ctx, userID, g.PortfolioID); err != nil {
		return nil, err
	}
	g.ID = goalID
	g.UserID = userID
	normalize(g)
	return uc.repo.Update(ctx, g)
}

func (uc *UseCase) Delete(ctx context.Context, email, goalID string) error {
	userID, err := uc.userID(ctx, email)
	if err != nil {
		return err
	}
	if _, err := uc.ownedGoal(ctx, userID, goalID); err != nil {
		return err
	}
	return uc.repo.Delete(ctx, goalID)
}
