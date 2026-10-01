package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	createPortfolio func(userID, name, description string, maxPortfolios int) (*portfoliodomain.Portfolio, error)
	addPosition     func(portfolioID, symbol, name string, shares, avgCost float64, maxPositions int) (*portfoliodomain.Position, error)
	getValueHistory func(userID string, from *time.Time) ([]*portfoliodomain.ValuePoint, error)
	// listPositionsByUser / getActivityByUser back the cross-portfolio endpoints.
	listPositionsByUser func(userID string) ([]*portfoliodomain.Position, error)
	getActivityByUser   func(userID string, limit int) ([]*portfoliodomain.Activity, error)
}

func (m *mockPortfolioRepo) CreatePortfolio(_ context.Context, userID, name, description string, maxPortfolios int) (*portfoliodomain.Portfolio, error) {
	if m.createPortfolio != nil {
		return m.createPortfolio(userID, name, description, maxPortfolios)
	}
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
func (m *mockPortfolioRepo) Add(
	_ context.Context, portfolioID, symbol, name string, shares, avgCost float64, maxPositions int,
) (*portfoliodomain.Position, error) {
	if m.addPosition != nil {
		return m.addPosition(portfolioID, symbol, name, shares, avgCost, maxPositions)
	}
	return &portfoliodomain.Position{ID: "pos-new", PortfolioID: portfolioID, Symbol: symbol, Name: name, Shares: shares, AvgCost: avgCost}, nil
}
func (m *mockPortfolioRepo) Remove(_ context.Context, _, _ string) error { return nil }
func (m *mockPortfolioRepo) Update(_ context.Context, _, _ string, _, _ *float64) (*portfoliodomain.Position, error) {
	return nil, nil
}
func (m *mockPortfolioRepo) ListByPortfolioID(_ context.Context, _ string) ([]*portfoliodomain.Position, error) {
	return []*portfoliodomain.Position{}, nil
}
func (m *mockPortfolioRepo) ListPositionsByUser(_ context.Context, userID string) ([]*portfoliodomain.Position, error) {
	if m.listPositionsByUser != nil {
		return m.listPositionsByUser(userID)
	}
	return []*portfoliodomain.Position{}, nil
}
func (m *mockPortfolioRepo) GetActivityByUser(_ context.Context, userID string, limit int) ([]*portfoliodomain.Activity, error) {
	if m.getActivityByUser != nil {
		return m.getActivityByUser(userID, limit)
	}
	return []*portfoliodomain.Activity{}, nil
}
func (m *mockPortfolioRepo) GetActivity(_ context.Context, _ string, _ int) ([]*portfoliodomain.Activity, error) {
	return []*portfoliodomain.Activity{}, nil
}
func (m *mockPortfolioRepo) ListAllHoldings(_ context.Context) ([]*portfoliodomain.UserHolding, error) {
	return []*portfoliodomain.UserHolding{}, nil
}
func (m *mockPortfolioRepo) UpsertValueSnapshots(_ context.Context, _ time.Time, _ map[string]float64) error {
	return nil
}
func (m *mockPortfolioRepo) GetValueHistory(_ context.Context, userID string, from *time.Time) ([]*portfoliodomain.ValuePoint, error) {
	if m.getValueHistory != nil {
		return m.getValueHistory(userID, from)
	}
	return []*portfoliodomain.ValuePoint{}, nil
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
		{
			"limit reached",
			`{"name":"Growth"}`,
			&mockPortfolioRepo{createPortfolio: func(string, string, string, int) (*portfoliodomain.Portfolio, error) {
				return nil, portfoliodomain.ErrPortfolioLimitReached
			}},
			http.StatusConflict,
		},
		{"success", `{"name":"Growth"}`, &mockPortfolioRepo{}, http.StatusCreated},
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

// TestCrossPortfolioRoutes proves the static /positions and /activity routes are served
// for the authenticated user and are not swallowed by /:id (which would 404 here, since
// the mock knows no portfolio).
func TestCrossPortfolioRoutes(t *testing.T) {
	var gotLimit int
	repo := &mockPortfolioRepo{
		listPositionsByUser: func(userID string) ([]*portfoliodomain.Position, error) {
			if userID != testUserID {
				t.Fatalf("expected positions for %q, got %q", testUserID, userID)
			}
			return []*portfoliodomain.Position{}, nil
		},
		getActivityByUser: func(userID string, limit int) ([]*portfoliodomain.Activity, error) {
			if userID != testUserID {
				t.Fatalf("expected activity for %q, got %q", testUserID, userID)
			}
			gotLimit = limit
			return []*portfoliodomain.Activity{{ID: "a1", Label: "Bought VOO", PortfolioID: "pf1", PortfolioName: "Core"}}, nil
		},
	}
	tests := []struct {
		name      string
		url       string
		wantCode  int
		wantLimit int
		wantBody  string
	}{
		{"all positions", "/portfolios/positions", http.StatusOK, 0, `"result":[]`},
		{"activity defaults to 10", "/portfolios/activity", http.StatusOK, 10, `"portfolioName":"Core"`},
		{"activity explicit limit", "/portfolios/activity?limit=6", http.StatusOK, 6, `"portfolioName":"Core"`},
		{"activity limit too large 400", "/portfolios/activity?limit=51", http.StatusBadRequest, 0, ""},
		{"activity limit not a number 400", "/portfolios/activity?limit=abc", http.StatusBadRequest, 0, ""},
		{"activity negative limit 400", "/portfolios/activity?limit=-1", http.StatusBadRequest, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotLimit = 0
			r := newPortfolioRouter(repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))

			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
			if gotLimit != tc.wantLimit {
				t.Fatalf("expected limit %d passed to the repository, got %d", tc.wantLimit, gotLimit)
			}
			if tc.wantBody != "" && !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Fatalf("expected body to contain %s, got %s", tc.wantBody, w.Body.String())
			}
		})
	}
}

func TestGetValueHistoryHandler(t *testing.T) {
	// Each case reports whether the repository was reached and with what lower bound, so
	// the test also proves /history is not swallowed by the /:id route.
	tests := []struct {
		name      string
		url       string
		wantCode  int
		wantRange string
		wantBound bool
	}{
		{"defaults to 30D", "/portfolios/history", http.StatusOK, `"range":"30D"`, true},
		{"explicit range", "/portfolios/history?range=1Y", http.StatusOK, `"range":"1Y"`, true},
		{"All has no lower bound", "/portfolios/history?range=All", http.StatusOK, `"range":"All"`, false},
		{"unknown range 400", "/portfolios/history?range=5Y", http.StatusBadRequest, "", false},
		{"wrong case 400", "/portfolios/history?range=all", http.StatusBadRequest, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var called, bounded bool
			repo := &mockPortfolioRepo{getValueHistory: func(userID string, from *time.Time) ([]*portfoliodomain.ValuePoint, error) {
				called, bounded = true, from != nil
				if userID != testUserID {
					t.Fatalf("expected history for %q, got %q", testUserID, userID)
				}
				return []*portfoliodomain.ValuePoint{{Date: "2026-09-30", Value: 1234.5}}, nil
			}}
			r := newPortfolioRouter(repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))

			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
			if tc.wantCode != http.StatusOK {
				if called {
					t.Fatal("repository must not be queried for an invalid range")
				}
				return
			}
			body := w.Body.String()
			if !strings.Contains(body, tc.wantRange) || !strings.Contains(body, `"date":"2026-09-30"`) || !strings.Contains(body, `"value":1234.5`) {
				t.Fatalf("unexpected body: %s", body)
			}
			if !called || bounded != tc.wantBound {
				t.Fatalf("expected repository called with bounded=%v, got called=%v bounded=%v", tc.wantBound, called, bounded)
			}
		})
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
			&mockPortfolioRepo{
				getPortfolio: ownedPortfolio,
				addPosition: func(string, string, string, float64, float64, int) (*portfoliodomain.Position, error) {
					return nil, portfoliodomain.ErrPositionLimitReached
				},
			},
			http.StatusConflict,
		},
		{
			"success 201",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
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
