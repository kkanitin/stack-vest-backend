package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
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

	// Ledger overrides (SV-1); lastIncludeClosed records the last ListByPortfolioID flag.
	listTransactions  func(portfolioID, symbol string) ([]*portfoliodomain.Transaction, error)
	getTransaction    func(portfolioID, txID string) (*portfoliodomain.Transaction, error)
	createTransaction func(portfolioID string, t *portfoliodomain.Transaction, maxPositions int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error)
	updateTransaction func(t *portfoliodomain.Transaction) (*portfoliodomain.Transaction, *portfoliodomain.Position, error)
	deleteTransaction func(portfolioID, txID string) error
	lastIncludeClosed bool
	// listPositions overrides ListByPortfolioID (used by the legacy add-position check).
	listPositions func(portfolioID string) ([]*portfoliodomain.Position, error)
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
func (m *mockPortfolioRepo) ListByPortfolioID(_ context.Context, portfolioID string, includeClosed bool) ([]*portfoliodomain.Position, error) {
	m.lastIncludeClosed = includeClosed
	if m.listPositions != nil {
		return m.listPositions(portfolioID)
	}
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

// stubIndexHistory is the benchmark index fetcher: it returns fixed closes, or err.
type stubIndexHistory struct {
	closes []stockdomain.HistoryPoint
	err    error
	calls  int
}

func (s *stubIndexHistory) GetHistoryClose(string, time.Time, time.Time) ([]stockdomain.HistoryPoint, error) {
	s.calls++
	return s.closes, s.err
}

func newPortfolioRouter(repo portfoliodomain.Repository) *gin.Engine {
	return newPortfolioRouterWithIndex(repo, &stubIndexHistory{})
}

func newPortfolioRouterWithIndex(repo portfoliodomain.Repository, index stockdomain.HistoryFetcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.EmailKey, "test@example.com")
		c.Next()
	})
	userRepo := &mockUserRepo{findByEmailFn: func(_ context.Context, _ string) (*userdomain.User, error) {
		return &userdomain.User{ID: testUserID, Email: "test@example.com"}, nil
	}}
	uc := portfoliouc.New(repo, userRepo, nil, nil, 10, 20).WithBenchmarks(index, portfoliodomain.ParseBenchmarks(
		[]string{"SPY=S&P 500", "QQQ=Nasdaq 100", "VT=Total world"},
	))
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

func TestGetValueHistoryHandler_Benchmark(t *testing.T) {
	twoDays := func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		return []*portfoliodomain.ValuePoint{{Date: "2026-09-12", Value: 10}, {Date: "2026-09-14", Value: 11}}, nil
	}
	closes := []stockdomain.HistoryPoint{{Date: "2026-09-11", Close: 540.12}, {Date: "2026-09-14", Close: 545}}

	t.Run("valid symbol 200 with aligned closes", func(t *testing.T) {
		r := newPortfolioRouterWithIndex(&mockPortfolioRepo{getValueHistory: twoDays}, &stubIndexHistory{closes: closes})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/history?range=30D&benchmark=SPY", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
		}
		for _, want := range []string{
			`"date":"2026-09-12","value":10,"benchmarkClose":540.12`,
			`"date":"2026-09-14","value":11,"benchmarkClose":545`,
			`"benchmark":{"symbol":"SPY","label":"S\u0026P 500","available":true}`,
		} {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("expected body to contain %s, got %s", want, w.Body.String())
			}
		}
	})

	t.Run("index failure still 200 and unavailable", func(t *testing.T) {
		r := newPortfolioRouterWithIndex(&mockPortfolioRepo{getValueHistory: twoDays}, &stubIndexHistory{err: errors.New("fmp down")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/history?benchmark=VT", nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, `"benchmark":{"symbol":"VT","label":"Total world","available":false}`) || strings.Contains(body, "benchmarkClose") {
			t.Fatalf("unexpected response %d: %s", w.Code, body)
		}
	})

	t.Run("no benchmark param adds no benchmark fields", func(t *testing.T) {
		index := &stubIndexHistory{closes: closes}
		r := newPortfolioRouterWithIndex(&mockPortfolioRepo{getValueHistory: twoDays}, index)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/history", nil))
		const want = `"result":{"range":"30D","points":[{"date":"2026-09-12","value":10,"returnPct":0},{"date":"2026-09-14","value":11,"returnPct":0}]},`
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) || index.calls != 0 {
			t.Fatalf("unexpected response %d: %s (index calls=%d)", w.Code, w.Body.String(), index.calls)
		}
	})

	for _, sym := range []string{"AAPL", "spy", "SPY%20"} {
		t.Run("rejects "+sym, func(t *testing.T) {
			called := false
			repo := &mockPortfolioRepo{getValueHistory: func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) {
				called = true
				return nil, nil
			}}
			r := newPortfolioRouter(repo)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/history?benchmark="+sym, nil))
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "benchmark must be one of: SPY, QQQ, VT") {
				t.Fatalf("expected 400 naming the configured symbols, got %d: %s", w.Code, w.Body.String())
			}
			if called {
				t.Fatal("repository must not be queried for an unknown benchmark")
			}
		})
	}
}

// TestListBenchmarksHandler also proves /benchmarks is not swallowed by /:id (which would
// 404 here, since the mock knows no portfolio).
func TestListBenchmarksHandler(t *testing.T) {
	r := newPortfolioRouter(&mockPortfolioRepo{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/benchmarks", nil))
	// Go's JSON encoder escapes the ampersand, which decodes to the same string.
	want := `"result":[{"symbol":"SPY","label":"S` + `\` + `u0026P 500"},{"symbol":"QQQ","label":"Nasdaq 100"},{"symbol":"VT","label":"Total world"}]`
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("unexpected response %d: %s", w.Code, w.Body.String())
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
				createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
					return nil, nil, portfoliodomain.ErrPositionLimitReached
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
		{
			"open position already exists 409",
			`{"symbol":"aapl","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{
				getPortfolio: ownedPortfolio,
				listPositions: func(string) ([]*portfoliodomain.Position, error) {
					return []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 2}}, nil
				},
			},
			http.StatusConflict,
		},
		{
			"closed position may reopen 201",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{
				getPortfolio: ownedPortfolio,
				listPositions: func(string) ([]*portfoliodomain.Position, error) {
					return []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 0, Closed: true}}, nil
				},
			},
			http.StatusCreated,
		},
		{
			"whitespace symbol 400",
			`{"symbol":"   ","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
		{
			"whitespace name 400",
			`{"symbol":"AAPL","name":"  ","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
		{
			"overflowing shares 400",
			`{"symbol":"AAPL","name":"Apple","shares":1e12,"avgCost":100}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio},
			http.StatusBadRequest,
		},
		{
			"invalid transaction from repo 400",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{
				getPortfolio: ownedPortfolio,
				createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
					return nil, nil, portfoliodomain.ErrInvalidTransaction
				},
			},
			http.StatusBadRequest,
		},
		{
			"future date from repo 400",
			`{"symbol":"AAPL","name":"Apple","shares":1,"avgCost":100}`,
			&mockPortfolioRepo{
				getPortfolio: ownedPortfolio,
				createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
					return nil, nil, portfoliodomain.ErrFutureDate
				},
			},
			http.StatusBadRequest,
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

// Ledger stubs: overridden by the tests that exercise transactions.
func (m *mockPortfolioRepo) ListTransactions(_ context.Context, portfolioID, symbol string) ([]*portfoliodomain.Transaction, error) {
	if m.listTransactions != nil {
		return m.listTransactions(portfolioID, symbol)
	}
	return []*portfoliodomain.Transaction{}, nil
}
func (m *mockPortfolioRepo) ListTransactionsByUser(_ context.Context, _ string) ([]*portfoliodomain.Transaction, error) {
	return []*portfoliodomain.Transaction{}, nil
}
func (m *mockPortfolioRepo) CreateTransaction(_ context.Context, portfolioID string, t *portfoliodomain.Transaction, maxPositions int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
	if m.createTransaction != nil {
		return m.createTransaction(portfolioID, t, maxPositions)
	}
	out := *t
	out.ID = "tx-new"
	return &out, &portfoliodomain.Position{ID: "pos-new", PortfolioID: portfolioID, Symbol: t.Symbol, Name: t.Name, Shares: t.Quantity, AvgCost: t.Price}, nil
}
func (m *mockPortfolioRepo) UpdateTransaction(ctx context.Context, portfolioID, txID string, patch portfoliodomain.TransactionPatch, _ int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
	stored, err := m.GetTransaction(ctx, portfolioID, txID)
	if err != nil {
		return nil, nil, err
	}
	merged := *stored
	patch.Apply(&merged)
	if m.updateTransaction != nil {
		return m.updateTransaction(&merged)
	}
	return &merged, &portfoliodomain.Position{Symbol: merged.Symbol}, nil
}
func (m *mockPortfolioRepo) DeleteTransaction(_ context.Context, portfolioID, txID string, _ int) error {
	if m.deleteTransaction != nil {
		return m.deleteTransaction(portfolioID, txID)
	}
	return nil
}
func (m *mockPortfolioRepo) GetTransaction(_ context.Context, portfolioID, txID string) (*portfoliodomain.Transaction, error) {
	if m.getTransaction != nil {
		return m.getTransaction(portfolioID, txID)
	}
	return nil, portfoliodomain.ErrTransactionNotFound
}
