package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

// mockPortfolioRepo is a hand-written stub of portfoliodomain.Repository. Only the
// methods exercised by the handler paths under test carry overridable behaviour.
type mockPortfolioRepo struct {
	getPortfolio    func(id string) (*portfoliodomain.Portfolio, error)
	countPortfolios func(userID string) (int, error)
	countPositions  func(portfolioID string) (int, error)
}

func (m *mockPortfolioRepo) CreatePortfolio(_ context.Context, userID, name, description string) (*portfoliodomain.Portfolio, error) {
	return &portfoliodomain.Portfolio{ID: "pf-new", UserID: userID, Name: name, Description: description}, nil
}
func (m *mockPortfolioRepo) ListPortfolios(_ context.Context, _ string) ([]*portfoliodomain.Portfolio, error) {
	return []*portfoliodomain.Portfolio{}, nil
}
func (m *mockPortfolioRepo) GetPortfolio(_ context.Context, id string) (*portfoliodomain.Portfolio, error) {
	if m.getPortfolio != nil {
		return m.getPortfolio(id)
	}
	return nil, portfoliodomain.ErrPortfolioNotFound
}
func (m *mockPortfolioRepo) UpdatePortfolio(_ context.Context, id string, _, _ *string) (*portfoliodomain.Portfolio, error) {
	return &portfoliodomain.Portfolio{ID: id}, nil
}
func (m *mockPortfolioRepo) DeletePortfolio(_ context.Context, _ string) error { return nil }
func (m *mockPortfolioRepo) CountPortfolios(_ context.Context, userID string) (int, error) {
	if m.countPortfolios != nil {
		return m.countPortfolios(userID)
	}
	return 0, nil
}
func (m *mockPortfolioRepo) Add(_ context.Context, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	return &portfoliodomain.Position{ID: "pos-new", PortfolioID: portfolioID, Symbol: symbol, Name: name, Shares: shares, AvgCost: avgCost}, nil
}
func (m *mockPortfolioRepo) Remove(_ context.Context, _, _ string) error { return nil }
func (m *mockPortfolioRepo) Update(_ context.Context, _, _ string, _, _ *float64) (*portfoliodomain.Position, error) {
	return nil, nil
}
func (m *mockPortfolioRepo) ListByPortfolioID(_ context.Context, _ string) ([]*portfoliodomain.Position, error) {
	return []*portfoliodomain.Position{}, nil
}
func (m *mockPortfolioRepo) ListPositionsByUser(_ context.Context, _ string) ([]*portfoliodomain.Position, error) {
	return []*portfoliodomain.Position{}, nil
}
func (m *mockPortfolioRepo) CountPositions(_ context.Context, portfolioID string) (int, error) {
	if m.countPositions != nil {
		return m.countPositions(portfolioID)
	}
	return 0, nil
}
func (m *mockPortfolioRepo) GetActivity(_ context.Context, _ string, _ int) ([]*portfoliodomain.Activity, error) {
	return []*portfoliodomain.Activity{}, nil
}

const testUserID = "u1"

func newPortfolioRouter(repo portfoliodomain.Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.EmailKey, "test@example.com")
		c.Next()
	})
	userRepo := &mockUserRepo{findByEmailFn: func(_ context.Context, _ string) (*userdomain.User, error) {
		return &userdomain.User{ID: testUserID, Email: "test@example.com"}, nil
	}}
	uc := portfoliouc.New(repo, userRepo, nil, nil, 10, 20)
	NewPortfolioHandler(uc, nil).RegisterRoutes(r.Group(""))
	return r
}

func ownedPortfolio(id string) (*portfoliodomain.Portfolio, error) {
	return &portfoliodomain.Portfolio{ID: id, UserID: testUserID, Name: "Mine"}, nil
}

func TestCreatePortfolioHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		repo     *mockPortfolioRepo
		wantCode int
	}{
		{"missing name", `{"description":"x"}`, &mockPortfolioRepo{}, http.StatusBadRequest},
		{"limit reached", `{"name":"Growth"}`, &mockPortfolioRepo{countPortfolios: func(string) (int, error) { return 10, nil }}, http.StatusConflict},
		{"success", `{"name":"Growth"}`, &mockPortfolioRepo{countPortfolios: func(string) (int, error) { return 0, nil }}, http.StatusCreated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newPortfolioRouter(tc.repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/portfolios", strings.NewReader(tc.body)))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestGetPortfolioHandler_ForeignReturns404(t *testing.T) {
	repo := &mockPortfolioRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "another-user"}, nil
	}}
	r := newPortfolioRouter(repo)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/foreign-id", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another user's portfolio, got %d", w.Code)
	}
}

func TestGetPortfoliosSummaryHandler(t *testing.T) {
	r := newPortfolioRouter(&mockPortfolioRepo{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/summary", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for summary, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestAnalyzePortfolioHandler covers the pre-stream paths of POST /portfolios/{id}/analyze,
// which return before the analysis use case (nil here) or any pricing is touched. It also
// confirms the route is reachable and distinct from the stateless POST /portfolios/analyze.
func TestAnalyzePortfolioHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		repo     *mockPortfolioRepo
		wantCode int
	}{
		{
			"missing dimensions 400",
			`{}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
		{
			"foreign portfolio 404",
			`{"dimensions":["risk"]}`,
			&mockPortfolioRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
				return &portfoliodomain.Portfolio{ID: id, UserID: "another-user"}, nil
			}},
			http.StatusNotFound,
		},
		{
			// Owned portfolio with no holdings → ErrPortfolioEmpty (mock ListByPortfolioID
			// returns an empty slice) → nothing to analyze.
			"empty portfolio 400",
			`{"dimensions":["risk"]}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newPortfolioRouter(tc.repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/portfolios/pf1/analyze", strings.NewReader(tc.body)))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestAddPositionHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		repo     *mockPortfolioRepo
		wantCode int
	}{
		{
			"foreign portfolio 404",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
				return &portfoliodomain.Portfolio{ID: id, UserID: "another-user"}, nil
			}},
			http.StatusNotFound,
		},
		{
			"validation 400",
			`{"symbol":"AAPL","name":"Apple","shares":0,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
		{
			"position limit 409",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio, countPositions: func(string) (int, error) { return 20, nil }},
			http.StatusConflict,
		},
		{
			"success 201",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio, countPositions: func(string) (int, error) { return 0, nil }},
			http.StatusCreated,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newPortfolioRouter(tc.repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/portfolios/pf1/positions", strings.NewReader(tc.body)))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}
