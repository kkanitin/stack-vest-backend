package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	goaluc "github.com/kanitin/stackvest/backend/internal/usecase/goal"
)

// goalTestRepo is an in-memory goaldomain.Repository.
type goalTestRepo struct {
	goals  map[string]*goaldomain.Goal
	order  []string
	nextID int
}

func (r *goalTestRepo) Create(_ context.Context, g *goaldomain.Goal, max int) (*goaldomain.Goal, error) {
	n := 0
	for _, e := range r.goals {
		if e.UserID == g.UserID {
			n++
		}
	}
	if n >= max {
		return nil, goaldomain.ErrGoalLimitReached
	}
	r.nextID++
	c := *g
	c.ID = fmt.Sprintf("goal-%d", r.nextID)
	r.goals[c.ID] = &c
	r.order = append(r.order, c.ID)
	out := c
	return &out, nil
}

func (r *goalTestRepo) ListByUser(_ context.Context, userID string, limit, offset int) ([]*goaldomain.Goal, int, error) {
	mine := []*goaldomain.Goal{}
	for _, id := range r.order {
		if g, ok := r.goals[id]; ok && g.UserID == userID {
			mine = append(mine, g)
		}
	}
	total := len(mine)
	offset = min(offset, total)
	return mine[offset:min(offset+limit, total)], total, nil
}

func (r *goalTestRepo) Get(_ context.Context, id string) (*goaldomain.Goal, error) {
	g, ok := r.goals[id]
	if !ok {
		return nil, goaldomain.ErrGoalNotFound
	}
	c := *g
	return &c, nil
}

func (r *goalTestRepo) Update(_ context.Context, g *goaldomain.Goal) (*goaldomain.Goal, error) {
	c := *g
	r.goals[g.ID] = &c
	return &c, nil
}

func (r *goalTestRepo) Delete(_ context.Context, id string) error {
	delete(r.goals, id)
	return nil
}

type goalTestPortfolios map[string]string // portfolio ID → owner user ID

func (p goalTestPortfolios) GetPortfolio(_ context.Context, id string) (*portfoliodomain.Portfolio, error) {
	owner, ok := p[id]
	if !ok {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	return &portfoliodomain.Portfolio{ID: id, UserID: owner}, nil
}

// newGoalRouter serves the goal routes; the caller's email comes from the X-Test-Email
// header (default alice), standing in for the auth middleware.
func newGoalRouter(maxPerUser int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	users := &mockUserRepo{findByEmailFn: func(_ context.Context, email string) (*userdomain.User, error) {
		return &userdomain.User{ID: "u-" + strings.Split(email, "@")[0], Email: email}, nil
	}}
	uc := goaluc.New(
		&goalTestRepo{goals: map[string]*goaldomain.Goal{}}, users,
		goalTestPortfolios{"pf-alice": "u-alice", "pf-bob": "u-bob"}, maxPerUser,
	)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		email := c.GetHeader("X-Test-Email")
		if email == "" {
			email = "alice@example.com"
		}
		c.Set(middleware.EmailKey, email)
	})
	NewGoalHandler(uc).RegisterRoutes(r.Group("/api/v1"))
	return r
}

type goalEnvelope struct {
	Result       *goaldomain.Goal `json:"result"`
	Code         int              `json:"code"`
	ErrorMessage *string          `json:"errorMessage"`
}

type goalListEnvelope struct {
	Results []*goaldomain.Goal `json:"results"`
	Meta    struct {
		Total            int `json:"total"`
		Page             int `json:"page"`
		Size             int `json:"size"`
		CurrentPageCount int `json:"currentPageCount"`
	} `json:"meta"`
}

func doGoal(t *testing.T, r *gin.Engine, method, path, body, email string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("X-Test-Email", email)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func createGoal(t *testing.T, r *gin.Engine, body string) *goaldomain.Goal {
	t.Helper()
	w := doGoal(t, r, http.MethodPost, "/api/v1/goals", body, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", w.Code, w.Body.String())
	}
	var env goalEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	return env.Result
}

const projectionBody = `{"name":"Retirement","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7}`

func TestGoalHandler_CreateValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"valid projection", projectionBody, http.StatusCreated},
		{"valid target", `{"name":"House","kind":"target","targetAmount":1000000,"years":5,"expectedReturn":4}`, http.StatusCreated},
		{"expectedReturn 0 and monthlyAmount 0 are valid", `{"name":"Cash","kind":"projection","monthlyAmount":0,"years":1,"expectedReturn":0}`, http.StatusCreated},
		{"all optional fields", `{"name":"R","kind":"projection","currency":"thb","startingAmount":1000,"monthlyAmount":500,"years":10,"expectedReturn":7,"contributionGrowth":3,"inflation":2.5,"portfolioId":"pf-alice"}`, http.StatusCreated},
		{"missing name", `{"kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"blank name", `{"name":"   ","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"unknown kind", `{"name":"R","kind":"other","monthlyAmount":500,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"projection without monthlyAmount", `{"name":"R","kind":"projection","targetAmount":100,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"target without targetAmount", `{"name":"R","kind":"target","monthlyAmount":100,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"target with targetAmount 0", `{"name":"R","kind":"target","targetAmount":0,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"years 0", `{"name":"R","kind":"projection","monthlyAmount":500,"years":0,"expectedReturn":7}`, http.StatusBadRequest},
		{"years negative", `{"name":"R","kind":"projection","monthlyAmount":500,"years":-1,"expectedReturn":7}`, http.StatusBadRequest},
		{"missing years", `{"name":"R","kind":"projection","monthlyAmount":500,"expectedReturn":7}`, http.StatusBadRequest},
		{"missing expectedReturn", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30}`, http.StatusBadRequest},
		{"negative monthlyAmount", `{"name":"R","kind":"projection","monthlyAmount":-1,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"negative startingAmount", `{"name":"R","kind":"projection","startingAmount":-1,"monthlyAmount":1,"years":30,"expectedReturn":7}`, http.StatusBadRequest},
		{"someone else's portfolio", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"portfolioId":"pf-bob"}`, http.StatusBadRequest},
		{"unknown portfolio", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"portfolioId":"pf-nope"}`, http.StatusBadRequest},
		{"malformed JSON", `{"name":`, http.StatusBadRequest},
		{"volatility null", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":null}`, http.StatusCreated},
		{"volatility 0", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":0}`, http.StatusCreated},
		{"volatility 100", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":100}`, http.StatusCreated},
		{"volatility above 100", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":100.5}`, http.StatusBadRequest},
		{"volatility negative", `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":-1}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doGoal(t, newGoalRouter(50), http.MethodPost, "/api/v1/goals", tc.body, "")
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d; body %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestGoalHandler_CreateResponseShape(t *testing.T) {
	r := newGoalRouter(50)
	g := createGoal(t, r, `{"name":" Retirement ","kind":"projection","currency":"thb","monthlyAmount":500,"years":30,"expectedReturn":7}`)
	if g.ID == "" || g.Name != "Retirement" || g.Currency != "THB" || g.StartingAmount != 0 || g.PortfolioID != nil {
		t.Fatalf("unexpected goal: %+v", g)
	}

	w := doGoal(t, r, http.MethodPost, "/api/v1/goals", projectionBody, "")
	var raw struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "portfolioId", "startingAmount", "monthlyAmount", "targetAmount", "expectedReturn", "contributionGrowth", "volatility", "createdAt"} {
		if _, ok := raw.Result[key]; !ok {
			t.Errorf("result is missing %q", key)
		}
	}
	if _, ok := raw.Result["userId"]; ok {
		t.Error("result must not expose userId")
	}
}

func TestGoalHandler_DefaultCurrency(t *testing.T) {
	g := createGoal(t, newGoalRouter(50), projectionBody)
	if g.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", g.Currency)
	}
}

func TestGoalHandler_LimitReached(t *testing.T) {
	r := newGoalRouter(1)
	createGoal(t, r, projectionBody)
	if w := doGoal(t, r, http.MethodPost, "/api/v1/goals", projectionBody, ""); w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", w.Code)
	}
}

func TestGoalHandler_ListPagination(t *testing.T) {
	r := newGoalRouter(50)
	for i := 0; i < 3; i++ {
		createGoal(t, r, projectionBody)
	}
	doGoal(t, r, http.MethodPost, "/api/v1/goals", projectionBody, "bob@example.com")

	w := doGoal(t, r, http.MethodGet, "/api/v1/goals?page=2&size=2", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var env goalListEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Meta.Total != 3 || env.Meta.Page != 2 || env.Meta.Size != 2 || env.Meta.CurrentPageCount != 1 || len(env.Results) != 1 {
		t.Fatalf("unexpected page: %+v (%d results)", env.Meta, len(env.Results))
	}

	if w := doGoal(t, r, http.MethodGet, "/api/v1/goals?size=101", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("size=101: status %d, want 400", w.Code)
	}

	// A user with no goals gets an empty array, not null.
	w = doGoal(t, r, http.MethodGet, "/api/v1/goals", "", "carol@example.com")
	if !strings.Contains(w.Body.String(), `"results":[]`) {
		t.Fatalf("want empty results array, got %s", w.Body.String())
	}
}

func TestGoalHandler_GetUpdateDelete(t *testing.T) {
	r := newGoalRouter(50)
	g := createGoal(t, r, projectionBody)
	path := "/api/v1/goals/" + g.ID

	if w := doGoal(t, r, http.MethodGet, path, "", ""); w.Code != http.StatusOK {
		t.Fatalf("get: status %d", w.Code)
	}

	w := doGoal(t, r, http.MethodPut, path, `{"name":"House","kind":"target","targetAmount":2000000,"years":8,"expectedReturn":5,"portfolioId":"pf-alice"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("put: status %d, body %s", w.Code, w.Body.String())
	}
	var env goalEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Name != "House" || env.Result.Kind != goaldomain.KindTarget || env.Result.PortfolioID == nil {
		t.Fatalf("unexpected update: %+v", env.Result)
	}

	if w := doGoal(t, r, http.MethodPut, path, `{"name":"House","kind":"target","years":8,"expectedReturn":5}`, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("put without targetAmount: status %d, want 400", w.Code)
	}

	if w := doGoal(t, r, http.MethodDelete, path, "", ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", w.Code)
	}
	if w := doGoal(t, r, http.MethodGet, path, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status %d, want 404", w.Code)
	}
}

func TestGoalHandler_OtherUserGets404(t *testing.T) {
	r := newGoalRouter(50)
	g := createGoal(t, r, projectionBody)
	path := "/api/v1/goals/" + g.ID
	const bob = "bob@example.com"

	if w := doGoal(t, r, http.MethodGet, path, "", bob); w.Code != http.StatusNotFound {
		t.Fatalf("get: status %d, want 404", w.Code)
	}
	if w := doGoal(t, r, http.MethodPut, path, projectionBody, bob); w.Code != http.StatusNotFound {
		t.Fatalf("put: status %d, want 404", w.Code)
	}
	if w := doGoal(t, r, http.MethodDelete, path, "", bob); w.Code != http.StatusNotFound {
		t.Fatalf("delete: status %d, want 404", w.Code)
	}
	if w := doGoal(t, r, http.MethodGet, path, "", ""); w.Code != http.StatusOK {
		t.Fatalf("owner lost the goal: status %d", w.Code)
	}
	if w := doGoal(t, r, http.MethodGet, "/api/v1/goals/does-not-exist", "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("missing goal: status %d, want 404", w.Code)
	}
}

func TestGoalHandler_VolatilityRoundTrip(t *testing.T) {
	r := newGoalRouter(50)
	g := createGoal(t, r, `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":0}`)
	if g.Volatility == nil || *g.Volatility != 0 {
		t.Fatalf("create should keep volatility 0 (not drop it): %v", g.Volatility)
	}
	path := "/api/v1/goals/" + g.ID

	read := func(method, body string) *goaldomain.Goal {
		t.Helper()
		w := doGoal(t, r, method, path, body, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", method, w.Code, w.Body.String())
		}
		var env goalEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		return env.Result
	}

	if got := read(http.MethodPut, `{"name":"R","kind":"projection","monthlyAmount":500,"years":30,"expectedReturn":7,"volatility":18.5}`); got.Volatility == nil || *got.Volatility != 18.5 {
		t.Fatalf("put volatility 18.5: got %v", got.Volatility)
	}
	if got := read(http.MethodGet, ""); got.Volatility == nil || *got.Volatility != 18.5 {
		t.Fatalf("get after put: got %v", got.Volatility)
	}
	// PUT replaces every field: leaving volatility out clears it.
	if got := read(http.MethodPut, projectionBody); got.Volatility != nil {
		t.Fatalf("put without volatility should clear it, got %v", *got.Volatility)
	}

	w := doGoal(t, r, http.MethodGet, "/api/v1/goals", "", "")
	if !strings.Contains(w.Body.String(), `"volatility":null`) {
		t.Fatalf("list should return the volatility field: %s", w.Body.String())
	}
}
