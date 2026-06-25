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
	getPortfolio        func(id string) (*portfoliodomain.Portfolio, error)
	countPortfolios     func(userID string) (int, error)
	countPositions      func(portfolioID string) (int, error)
	listPortfolios      func(userID string) ([]*portfoliodomain.Portfolio, error)
	listPositionsByUser func(userID string) ([]*portfoliodomain.Position, error)
	createCalled        bool
	addCalled           bool
}

func (m *mockRepo) CreatePortfolio(_ context.Context, userID, name, description string) (*portfoliodomain.Portfolio, error) {
	m.createCalled = true
	return &portfoliodomain.Portfolio{ID: "new-pf", UserID: userID, Name: name, Description: description}, nil
}
func (m *mockRepo) ListPortfolios(_ context.Context, userID string) ([]*portfoliodomain.Portfolio, error) {
	if m.listPortfolios != nil {
		return m.listPortfolios(userID)
	}
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
func (m *mockRepo) ListPositionsByUser(_ context.Context, userID string) ([]*portfoliodomain.Position, error) {
	if m.listPositionsByUser != nil {
		return m.listPositionsByUser(userID)
	}
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

// stubQuoter / stubPriceChanger return fixed market data keyed by symbol so value
// and diversification math can be asserted deterministically.
type stubQuoter struct{ price map[string]float64 }

func (s stubQuoter) GetQuote(symbol string) (*stockdomain.Quote, error) {
	return &stockdomain.Quote{Symbol: symbol, Price: s.price[symbol]}, nil
}

type stubPriceChanger struct{ m1 map[string]float64 }

func (s stubPriceChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	return &stockdomain.PriceChange{M1: s.m1[symbol]}, nil
}

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

func newUCWithPrices(repo portfoliodomain.Repository, userRepo userdomain.Repository, q stockdomain.Quoter, pc stockdomain.PriceChanger) *portfoliouc.UseCase {
	return portfoliouc.New(repo, userRepo, q, pc, 10, 20)
}

// failingPriceChanger simulates an upstream outage so a held symbol has no usable price.
type failingPriceChanger struct{}

func (failingPriceChanger) GetPriceChange(string) (*stockdomain.PriceChange, error) {
	return nil, errors.New("upstream down")
}

func TestListPortfolios_PricingOutageLeavesValueNil(t *testing.T) {
	repo := &mockRepo{
		listPortfolios: func(string) ([]*portfoliodomain.Portfolio, error) {
			return []*portfoliodomain.Portfolio{{ID: "pf1", UserID: "u1"}}, nil
		},
		listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
			return []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 3}}, nil
		},
	}
	q := stubQuoter{price: map[string]float64{"AAPL": 100}}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, failingPriceChanger{})

	ps, err := uc.ListPortfolios(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Holdings exist but couldn't be priced: value is nil (→ "—"), count still known.
	if ps[0].Value != nil {
		t.Fatalf("expected nil value on pricing outage, got %v", *ps[0].Value)
	}
	if ps[0].AssetCount == nil || *ps[0].AssetCount != 1 {
		t.Fatalf("expected assetCount 1 despite outage, got %v", ps[0].AssetCount)
	}
}

func TestGetPortfoliosSummary_Empty(t *testing.T) {
	repo := &mockRepo{listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{}, nil
	}}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	s, err := uc.GetPortfoliosSummary(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.TotalValue != 0 || s.ChangePct != 0 || s.DiversificationScore != 0 {
		t.Fatalf("expected zero summary, got %+v", s)
	}
}

func TestGetPortfoliosSummary_AggregatesAndScores(t *testing.T) {
	// AAPL and MSFT each worth 1000 (10 × $100); both up 25% over the month.
	repo := &mockRepo{listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{
			{PortfolioID: "pf1", Symbol: "AAPL", Shares: 10},
			{PortfolioID: "pf2", Symbol: "MSFT", Shares: 10},
		}, nil
	}}
	q := stubQuoter{price: map[string]float64{"AAPL": 100, "MSFT": 100}}
	pc := stubPriceChanger{m1: map[string]float64{"AAPL": 25, "MSFT": 25}}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, pc)

	s, err := uc.GetPortfoliosSummary(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.TotalValue != 2000 {
		t.Fatalf("expected totalValue 2000, got %v", s.TotalValue)
	}
	if s.ChangePct < 24.99 || s.ChangePct > 25.01 {
		t.Fatalf("expected changePct ~25, got %v", s.ChangePct)
	}
	// Two equally-weighted symbols: HHI = 0.5, score = (1-0.5)*100 = 50.
	if s.DiversificationScore != 50 {
		t.Fatalf("expected diversificationScore 50, got %d", s.DiversificationScore)
	}
}

func TestGetPortfoliosSummary_SingleHoldingScoresZero(t *testing.T) {
	repo := &mockRepo{listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
		return []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 5}}, nil
	}}
	q := stubQuoter{price: map[string]float64{"AAPL": 200}}
	pc := stubPriceChanger{m1: map[string]float64{"AAPL": 0}}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, pc)

	s, err := uc.GetPortfoliosSummary(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.DiversificationScore != 0 {
		t.Fatalf("expected diversificationScore 0 for a single holding, got %d", s.DiversificationScore)
	}
}

func TestListPortfolios_EnrichesValueAndAssetCount(t *testing.T) {
	repo := &mockRepo{
		listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) {
			return []*portfoliodomain.Position{
				{PortfolioID: "pf1", Symbol: "AAPL", Shares: 2},
				{PortfolioID: "pf1", Symbol: "MSFT", Shares: 1},
			}, nil
		},
	}
	repo.listPortfolios = func(string) ([]*portfoliodomain.Portfolio, error) {
		return []*portfoliodomain.Portfolio{{ID: "pf1", UserID: "u1"}, {ID: "pf2", UserID: "u1"}}, nil
	}
	q := stubQuoter{price: map[string]float64{"AAPL": 100, "MSFT": 50}}
	pc := stubPriceChanger{m1: map[string]float64{"AAPL": 0, "MSFT": 0}}
	uc := newUCWithPrices(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, pc)

	ps, err := uc.ListPortfolios(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("expected 2 portfolios, got %d", len(ps))
	}
	// pf1: 2×100 + 1×50 = 250, 2 assets. pf2: empty.
	if ps[0].Value == nil || *ps[0].Value != 250 || ps[0].AssetCount == nil || *ps[0].AssetCount != 2 {
		t.Fatalf("pf1: expected value 250 / assetCount 2, got %v / %v", ps[0].Value, ps[0].AssetCount)
	}
	if ps[1].Value == nil || *ps[1].Value != 0 || ps[1].AssetCount == nil || *ps[1].AssetCount != 0 {
		t.Fatalf("pf2: expected empty (value 0 / assetCount 0), got %v / %v", ps[1].Value, ps[1].AssetCount)
	}
}
