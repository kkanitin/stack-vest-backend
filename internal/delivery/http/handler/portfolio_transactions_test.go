package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
)

const validTxBody = `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":5,"price":190,"date":"2024-03-01"}`

func doJSON(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.ServeHTTP(w, req)
	return w
}

func errorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		ErrorMessage *string `json:"errorMessage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.ErrorMessage == nil {
		t.Fatalf("expected an errorMessage, got %s", w.Body.String())
	}
	return *env.ErrorMessage
}

func TestCreateTransactionHandler(t *testing.T) {
	oversell := portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2026-03-02", Held: 3, Sold: 5}
	tests := []struct {
		name     string
		body     string
		repo     *mockPortfolioRepo
		wantCode int
	}{
		{"success 201", validTxBody, &mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusCreated},
		{"fee zero and note accepted", `{"symbol":"AAPL","name":"Apple","side":"sell","quantity":1,"price":0,"fee":0,"note":"x","date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusCreated},
		{"foreign portfolio 404", validTxBody, &mockPortfolioRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
			return &portfoliodomain.Portfolio{ID: id, UserID: "another-user"}, nil
		}}, http.StatusNotFound},
		{"missing portfolio 404", validTxBody, &mockPortfolioRepo{}, http.StatusNotFound},
		{"insufficient shares 409", validTxBody, &mockPortfolioRepo{
			getPortfolio: ownedPortfolio,
			createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
				return nil, nil, oversell
			},
		}, http.StatusConflict},
		{"position limit 409", validTxBody, &mockPortfolioRepo{
			getPortfolio: ownedPortfolio,
			createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
				return nil, nil, portfoliodomain.ErrPositionLimitReached
			},
		}, http.StatusConflict},
		{"future date 400", `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":5,"price":190,"date":"2999-01-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"bad date format 400", `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":5,"price":190,"date":"1/2/2024"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"missing side 400", `{"symbol":"AAPL","name":"Apple","quantity":5,"price":190,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"bad side 400", `{"symbol":"AAPL","name":"Apple","side":"hold","quantity":5,"price":190,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"zero quantity 400", `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":0,"price":190,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"missing price 400", `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":5,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"negative fee 400", `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":5,"price":1,"fee":-1,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"missing symbol 400", `{"name":"Apple","side":"buy","quantity":5,"price":1,"date":"2024-03-01"}`,
			&mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
		{"malformed json 400", `{`, &mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(newPortfolioRouter(tc.repo), http.MethodPost, "/portfolios/pf1/transactions", tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateTransactionHandler_ResponseShapeAndInsufficientMessage(t *testing.T) {
	w := doJSON(newPortfolioRouter(&mockPortfolioRepo{getPortfolio: ownedPortfolio}), http.MethodPost, "/portfolios/pf1/transactions", validTxBody)
	var env struct {
		Result struct {
			Transaction map[string]any `json:"transaction"`
			Position    map[string]any `json:"position"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Transaction["id"] != "tx-new" || env.Result.Transaction["side"] != "buy" || env.Result.Transaction["date"] != "2024-03-01" {
		t.Fatalf("unexpected transaction %v", env.Result.Transaction)
	}
	if env.Result.Position["symbol"] != "AAPL" {
		t.Fatalf("unexpected position %v", env.Result.Position)
	}

	oversell := portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2026-03-02", Held: 3, Sold: 5}
	repo := &mockPortfolioRepo{
		getPortfolio: ownedPortfolio,
		createTransaction: func(string, *portfoliodomain.Transaction, int) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
			return nil, nil, oversell
		},
	}
	w = doJSON(newPortfolioRouter(repo), http.MethodPost, "/portfolios/pf1/transactions", validTxBody)
	if got := errorMessage(t, w); got != "This would leave you selling 5 AAPL on 2026-03-02 when you held 3." {
		t.Fatalf("unexpected 409 message %q", got)
	}
}

func storedTx() (*portfoliodomain.Transaction, error) {
	return &portfoliodomain.Transaction{
		ID: "tx1", PortfolioID: "pf1", Symbol: "AAPL", Name: "Apple", Side: "buy", Quantity: 10, Price: 100, Date: "2024-01-02",
	}, nil
}

func TestUpdateTransactionHandler(t *testing.T) {
	stored := func(string, string) (*portfoliodomain.Transaction, error) { return storedTx() }
	tests := []struct {
		name     string
		body     string
		repo     *mockPortfolioRepo
		wantCode int
	}{
		{"success 200", `{"quantity":8,"fee":0}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio, getTransaction: stored}, http.StatusOK},
		{"missing transaction 404", `{"quantity":8}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio}, http.StatusNotFound},
		{"foreign portfolio 404", `{"quantity":8}`, &mockPortfolioRepo{getTransaction: stored}, http.StatusNotFound},
		{"insufficient shares 409", `{"quantity":1}`, &mockPortfolioRepo{
			getPortfolio: ownedPortfolio, getTransaction: stored,
			updateTransaction: func(*portfoliodomain.Transaction) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
				return nil, nil, portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2024-02-01", Held: 1, Sold: 4}
			},
		}, http.StatusConflict},
		{"empty patch 400", `{}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio, getTransaction: stored}, http.StatusBadRequest},
		{"zero quantity 400", `{"quantity":0}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio, getTransaction: stored}, http.StatusBadRequest},
		{"bad side 400", `{"side":"hold"}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio, getTransaction: stored}, http.StatusBadRequest},
		{"future date 400", `{"date":"2999-01-01"}`, &mockPortfolioRepo{getPortfolio: ownedPortfolio, getTransaction: stored}, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(newPortfolioRouter(tc.repo), http.MethodPatch, "/portfolios/pf1/transactions/tx1", tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (body=%s)", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestDeleteTransactionHandler(t *testing.T) {
	var gotPf, gotTx string
	repo := &mockPortfolioRepo{getPortfolio: ownedPortfolio, deleteTransaction: func(pf, tx string) error {
		gotPf, gotTx = pf, tx
		return nil
	}}
	if w := doJSON(newPortfolioRouter(repo), http.MethodDelete, "/portfolios/pf1/transactions/tx9", ""); w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	if gotPf != "pf1" || gotTx != "tx9" {
		t.Fatalf("unexpected ids %s/%s", gotPf, gotTx)
	}

	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"missing 404", portfoliodomain.ErrTransactionNotFound, http.StatusNotFound},
		{"uncovered sell 409", portfoliodomain.ErrInsufficientShares{Symbol: "AAPL", Date: "2024-02-01", Held: 0, Sold: 4}, http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockPortfolioRepo{getPortfolio: ownedPortfolio, deleteTransaction: func(string, string) error { return tc.err }}
			if w := doJSON(newPortfolioRouter(repo), http.MethodDelete, "/portfolios/pf1/transactions/tx9", ""); w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
	if w := doJSON(newPortfolioRouter(&mockPortfolioRepo{}), http.MethodDelete, "/portfolios/pf1/transactions/tx9", ""); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown portfolio, got %d", w.Code)
	}
}

func TestListTransactionsHandler(t *testing.T) {
	var gotSymbol string
	repo := &mockPortfolioRepo{
		getPortfolio: ownedPortfolio,
		listTransactions: func(_, symbol string) ([]*portfoliodomain.Transaction, error) {
			gotSymbol = symbol
			return []*portfoliodomain.Transaction{
				{ID: "a2", Symbol: "AAPL", Side: "sell", Quantity: 4, Price: 120, Date: "2024-02-01"},
				{ID: "a1", Symbol: "AAPL", Side: "buy", Quantity: 10, Price: 100, Date: "2024-01-02"},
			}, nil
		},
	}
	r := newPortfolioRouter(repo)
	w := doJSON(r, http.MethodGet, "/portfolios/pf1/transactions?symbol=AAPL&page=1&size=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var env struct {
		Results []map[string]any `json:"results"`
		Meta    struct {
			Total, Page, Size, CurrentPageCount int
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if gotSymbol != "AAPL" || len(env.Results) != 1 || env.Results[0]["id"] != "a2" || env.Results[0]["runningShares"] != float64(6) {
		t.Fatalf("symbol=%q results=%v", gotSymbol, env.Results)
	}
	if env.Meta.Total != 2 || env.Meta.Page != 1 || env.Meta.Size != 1 || env.Meta.CurrentPageCount != 1 {
		t.Fatalf("unexpected meta %+v", env.Meta)
	}

	if w := doJSON(r, http.MethodGet, "/portfolios/pf1/transactions?size=500", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for size=500, got %d", w.Code)
	}
	if w := doJSON(newPortfolioRouter(&mockPortfolioRepo{}), http.MethodGet, "/portfolios/pf1/transactions", ""); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown portfolio, got %d", w.Code)
	}
}

func TestUpdatePositionHandler_Gone(t *testing.T) {
	r := newPortfolioRouter(&mockPortfolioRepo{getPortfolio: ownedPortfolio})
	w := doJSON(r, http.MethodPatch, "/portfolios/pf1/positions/AAPL", `{"shares":3}`)
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d (%s)", w.Code, w.Body.String())
	}
	if got := errorMessage(t, w); got != "Holdings are now edited through their transactions." {
		t.Fatalf("unexpected message %q", got)
	}
	if w := doJSON(newPortfolioRouter(&mockPortfolioRepo{}), http.MethodPatch, "/portfolios/pf1/positions/AAPL", `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown portfolio, got %d", w.Code)
	}
}

func TestListPositionsHandler_IncludeClosed(t *testing.T) {
	repo := &mockPortfolioRepo{getPortfolio: ownedPortfolio}
	r := newPortfolioRouter(repo)
	if w := doJSON(r, http.MethodGet, "/portfolios/pf1/positions?includeClosed=true", ""); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !repo.lastIncludeClosed {
		t.Fatal("includeClosed=true must reach the repo")
	}
	if w := doJSON(r, http.MethodGet, "/portfolios/pf1/positions", ""); w.Code != http.StatusOK || repo.lastIncludeClosed {
		t.Fatalf("default must exclude closed (code=%d includeClosed=%v)", w.Code, repo.lastIncludeClosed)
	}
	if w := doJSON(r, http.MethodGet, "/portfolios/pf1/positions?includeClosed=maybe", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a bad flag, got %d", w.Code)
	}
}

// A path id that is not a UUID cannot match a row; the repository reports it as
// ErrTransactionNotFound (Postgres 22P02 mapped there), which must answer 404, not 500.
func TestTransactionRoutes_MalformedTxIDIs404(t *testing.T) {
	notFound := func(string, string) (*portfoliodomain.Transaction, error) {
		return nil, portfoliodomain.ErrTransactionNotFound
	}
	repo := &mockPortfolioRepo{
		getPortfolio:      ownedPortfolio,
		getTransaction:    notFound,
		deleteTransaction: func(string, string) error { return portfoliodomain.ErrTransactionNotFound },
	}
	if w := doJSON(newPortfolioRouter(repo), http.MethodPatch, "/portfolios/pf1/transactions/not-a-uuid", `{"quantity":1}`); w.Code != http.StatusNotFound {
		t.Fatalf("PATCH: expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if w := doJSON(newPortfolioRouter(repo), http.MethodDelete, "/portfolios/pf1/transactions/not-a-uuid", ""); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE: expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestCreateTransactionHandler_OverflowIs400(t *testing.T) {
	body := `{"symbol":"AAPL","name":"Apple","side":"buy","quantity":1e12,"price":1,"date":"2024-01-02"}`
	w := doJSON(newPortfolioRouter(&mockPortfolioRepo{getPortfolio: ownedPortfolio}), http.MethodPost, "/portfolios/pf1/transactions", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
}
