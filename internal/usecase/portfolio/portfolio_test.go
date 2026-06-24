package portfolio_test

import (
	"context"
	"errors"
	"testing"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

// --- mocks ---

type mockUserRepo struct {
	user *userdomain.User
	err  error
}

func (m *mockUserRepo) FindByEmail(_ context.Context, _ string) (*userdomain.User, error) {
	return m.user, m.err
}
func (m *mockUserRepo) FindByGoogleID(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (m *mockUserRepo) Upsert(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}
func (m *mockUserRepo) Create(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}

type mockRepo struct {
	getPortfolio    func(id string) (*portfoliodomain.Portfolio, error)
	countPortfolios func(userID string) (int, error)
	countPositions  func(portfolioID string) (int, error)
	createCalled    bool
	addCalled       bool
}

func (m *mockRepo) CreatePortfolio(_ context.Context, userID, name, description string) (*portfoliodomain.Portfolio, error) {
	m.createCalled = true
	return &portfoliodomain.Portfolio{ID: "new-pf", UserID: userID, Name: name, Description: description}, nil
}
func (m *mockRepo) ListPortfolios(_ context.Context, _ string) ([]*portfoliodomain.Portfolio, error) {
	return nil, nil
}
func (m *mockRepo) GetPortfolio(_ context.Context, id string) (*portfoliodomain.Portfolio, error) {
	if m.getPortfolio != nil {
		return m.getPortfolio(id)
	}
	return nil, portfoliodomain.ErrPortfolioNotFound
}
func (m *mockRepo) UpdatePortfolio(_ context.Context, id string, _, _ *string) (*portfoliodomain.Portfolio, error) {
	return &portfoliodomain.Portfolio{ID: id}, nil
}
func (m *mockRepo) DeletePortfolio(_ context.Context, _ string) error { return nil }
func (m *mockRepo) CountPortfolios(_ context.Context, userID string) (int, error) {
	if m.countPortfolios != nil {
		return m.countPortfolios(userID)
	}
	return 0, nil
}
func (m *mockRepo) Add(_ context.Context, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	m.addCalled = true
	return &portfoliodomain.Position{ID: "new-pos", PortfolioID: portfolioID, Symbol: symbol, Name: name, Shares: shares, AvgCost: avgCost}, nil
}
func (m *mockRepo) Remove(_ context.Context, _, _ string) error { return nil }
func (m *mockRepo) Update(_ context.Context, _, _ string, _, _ *float64) (*portfoliodomain.Position, error) {
	return nil, nil
}
func (m *mockRepo) ListByPortfolioID(_ context.Context, _ string) ([]*portfoliodomain.Position, error) {
	return nil, nil
}
func (m *mockRepo) CountPositions(_ context.Context, portfolioID string) (int, error) {
	if m.countPositions != nil {
		return m.countPositions(portfolioID)
	}
	return 0, nil
}
func (m *mockRepo) GetActivity(_ context.Context, _ string, _ int) ([]*portfoliodomain.Activity, error) {
	return nil, nil
}

var _ portfoliodomain.Repository = (*mockRepo)(nil)

// quoter/priceChanger are unused by the paths under test; nil is fine since those
// methods are never reached.
func newUC(repo portfoliodomain.Repository, userRepo userdomain.Repository, maxPortfolios, maxPositions int) *portfoliouc.UseCase {
	var q stockdomain.Quoter
	var pc stockdomain.PriceChanger
	return portfoliouc.New(repo, userRepo, q, pc, maxPortfolios, maxPositions)
}

// --- tests ---

func TestCreatePortfolio_LimitReached(t *testing.T) {
	repo := &mockRepo{countPortfolios: func(string) (int, error) { return 10, nil }}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	_, err := uc.CreatePortfolio(context.Background(), "a@b.com", "Growth", "")
	if !errors.Is(err, portfoliodomain.ErrPortfolioLimitReached) {
		t.Fatalf("expected ErrPortfolioLimitReached, got %v", err)
	}
	if repo.createCalled {
		t.Fatal("CreatePortfolio should not be called when limit is reached")
	}
}

func TestCreatePortfolio_UnderLimit(t *testing.T) {
	repo := &mockRepo{countPortfolios: func(string) (int, error) { return 3, nil }}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	p, err := uc.CreatePortfolio(context.Background(), "a@b.com", "Growth", "long term")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.createCalled || p.Name != "Growth" {
		t.Fatalf("expected created portfolio, got %+v (called=%v)", p, repo.createCalled)
	}
}

func TestAddPosition_PortfolioNotOwned(t *testing.T) {
	repo := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "someone-else"}, nil
	}}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	_, err := uc.AddPosition(context.Background(), "a@b.com", "pf1", "AAPL", "Apple", 1, 100)
	if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound (404), got %v", err)
	}
	if repo.addCalled {
		t.Fatal("Add should not be called for a portfolio the user does not own")
	}
}

func TestAddPosition_PositionLimitReached(t *testing.T) {
	repo := &mockRepo{
		getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
			return &portfoliodomain.Portfolio{ID: id, UserID: "u1"}, nil
		},
		countPositions: func(string) (int, error) { return 20, nil },
	}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	_, err := uc.AddPosition(context.Background(), "a@b.com", "pf1", "AAPL", "Apple", 1, 100)
	if !errors.Is(err, portfoliodomain.ErrPositionLimitReached) {
		t.Fatalf("expected ErrPositionLimitReached, got %v", err)
	}
	if repo.addCalled {
		t.Fatal("Add should not be called when position limit is reached")
	}
}

func TestAddPosition_Success(t *testing.T) {
	repo := &mockRepo{
		getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
			return &portfoliodomain.Portfolio{ID: id, UserID: "u1"}, nil
		},
		countPositions: func(string) (int, error) { return 5, nil },
	}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	pos, err := uc.AddPosition(context.Background(), "a@b.com", "pf1", "AAPL", "Apple", 2, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.addCalled || pos.Symbol != "AAPL" {
		t.Fatalf("expected added position, got %+v (called=%v)", pos, repo.addCalled)
	}
}

func TestGetPortfolio_NotOwnedReturns404Error(t *testing.T) {
	repo := &mockRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "other"}, nil
	}}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	_, err := uc.GetPortfolio(context.Background(), "a@b.com", "pf1")
	if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound, got %v", err)
	}
}
