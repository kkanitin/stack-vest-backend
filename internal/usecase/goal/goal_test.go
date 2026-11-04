package goal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	goaluc "github.com/kanitin/stackvest/backend/internal/usecase/goal"
)

// --- fakes ---

// users maps email → user ID.
type fakeUsers map[string]string

func (f fakeUsers) FindByEmail(_ context.Context, email string) (*userdomain.User, error) {
	id, ok := f[email]
	if !ok {
		return nil, userdomain.ErrNotFound
	}
	return &userdomain.User{ID: id, Email: email}, nil
}
func (f fakeUsers) FindByGoogleID(context.Context, string) (*userdomain.User, error) { return nil, nil }
func (f fakeUsers) Upsert(context.Context, *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}
func (f fakeUsers) Create(context.Context, *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}

// fakePortfolios maps portfolio ID → owner user ID.
type fakePortfolios map[string]string

func (f fakePortfolios) GetPortfolio(_ context.Context, id string) (*portfoliodomain.Portfolio, error) {
	owner, ok := f[id]
	if !ok {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	return &portfoliodomain.Portfolio{ID: id, UserID: owner}, nil
}

type fakeRepo struct {
	goals   map[string]*goaldomain.Goal
	order   []string
	nextID  int
	deleted []string
}

func newFakeRepo() *fakeRepo { return &fakeRepo{goals: map[string]*goaldomain.Goal{}} }

func (r *fakeRepo) Create(_ context.Context, g *goaldomain.Goal, max int) (*goaldomain.Goal, error) {
	n := 0
	for _, existing := range r.goals {
		if existing.UserID == g.UserID {
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

func (r *fakeRepo) ListByUser(_ context.Context, userID string, limit, offset int) ([]*goaldomain.Goal, int, error) {
	var mine []*goaldomain.Goal
	for _, id := range r.order {
		if g, ok := r.goals[id]; ok && g.UserID == userID {
			mine = append(mine, g)
		}
	}
	total := len(mine)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return mine[offset:end], total, nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (*goaldomain.Goal, error) {
	g, ok := r.goals[id]
	if !ok {
		return nil, goaldomain.ErrGoalNotFound
	}
	c := *g
	return &c, nil
}

func (r *fakeRepo) Update(_ context.Context, g *goaldomain.Goal) (*goaldomain.Goal, error) {
	existing, ok := r.goals[g.ID]
	if !ok || existing.UserID != g.UserID {
		return nil, goaldomain.ErrGoalNotFound
	}
	c := *g
	r.goals[g.ID] = &c
	return &c, nil
}

func (r *fakeRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.goals[id]; !ok {
		return goaldomain.ErrGoalNotFound
	}
	delete(r.goals, id)
	r.deleted = append(r.deleted, id)
	return nil
}

// --- helpers ---

const (
	alice = "alice@example.com"
	bob   = "bob@example.com"
)

func f64(v float64) *float64 { return &v }
func str(v string) *string   { return &v }

func setup(max int) (*goaluc.UseCase, *fakeRepo) {
	repo := newFakeRepo()
	users := fakeUsers{alice: "u-alice", bob: "u-bob"}
	portfolios := fakePortfolios{"pf-alice": "u-alice", "pf-bob": "u-bob"}
	return goaluc.New(repo, users, portfolios, max), repo
}

func projection() *goaldomain.Goal {
	return &goaldomain.Goal{
		Name: "Retirement", Kind: goaldomain.KindProjection, MonthlyAmount: f64(500), Years: 30, ExpectedReturn: 7,
	}
}

// --- tests ---

func TestCreate_DefaultsCurrencyAndSetsOwner(t *testing.T) {
	uc, _ := setup(50)
	g, err := uc.Create(context.Background(), alice, projection())
	if err != nil {
		t.Fatal(err)
	}
	if g.UserID != "u-alice" || g.Currency != goaldomain.DefaultCurrency || g.ID == "" {
		t.Fatalf("unexpected goal: %+v", g)
	}
}

func TestCreate_KeepsGivenCurrency(t *testing.T) {
	uc, _ := setup(50)
	in := projection()
	in.Currency = "THB"
	g, err := uc.Create(context.Background(), alice, in)
	if err != nil || g.Currency != "THB" {
		t.Fatalf("got %+v, %v", g, err)
	}
}

func TestCreate_PortfolioOwnership(t *testing.T) {
	cases := []struct {
		name        string
		portfolioID *string
		wantErr     error
	}{
		{"own portfolio", str("pf-alice"), nil},
		{"no portfolio", nil, nil},
		{"someone else's portfolio", str("pf-bob"), goaldomain.ErrInvalidPortfolio},
		{"missing portfolio", str("pf-missing"), goaldomain.ErrInvalidPortfolio},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, _ := setup(50)
			in := projection()
			in.PortfolioID = tc.portfolioID
			_, err := uc.Create(context.Background(), alice, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCreate_LimitReached(t *testing.T) {
	uc, _ := setup(1)
	if _, err := uc.Create(context.Background(), alice, projection()); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Create(context.Background(), alice, projection()); !errors.Is(err, goaldomain.ErrGoalLimitReached) {
		t.Fatalf("err = %v, want ErrGoalLimitReached", err)
	}
	// The limit is per user.
	if _, err := uc.Create(context.Background(), bob, projection()); err != nil {
		t.Fatalf("bob blocked by alice's limit: %v", err)
	}
}

func TestList_OnlyOwnGoals(t *testing.T) {
	uc, _ := setup(50)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := uc.Create(ctx, alice, projection()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := uc.Create(ctx, bob, projection()); err != nil {
		t.Fatal(err)
	}
	goals, total, err := uc.List(ctx, alice, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(goals) != 2 {
		t.Fatalf("total=%d len=%d, want 3 and 2", total, len(goals))
	}
	for _, g := range goals {
		if g.UserID != "u-alice" {
			t.Fatalf("listed another user's goal: %+v", g)
		}
	}
}

func TestOwnership_OtherUserGetsNotFound(t *testing.T) {
	uc, repo := setup(50)
	ctx := context.Background()
	g, err := uc.Create(ctx, alice, projection())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := uc.Get(ctx, bob, g.ID); !errors.Is(err, goaldomain.ErrGoalNotFound) {
		t.Fatalf("Get: err = %v, want ErrGoalNotFound", err)
	}
	if _, err := uc.Update(ctx, bob, g.ID, projection()); !errors.Is(err, goaldomain.ErrGoalNotFound) {
		t.Fatalf("Update: err = %v, want ErrGoalNotFound", err)
	}
	if err := uc.Delete(ctx, bob, g.ID); !errors.Is(err, goaldomain.ErrGoalNotFound) {
		t.Fatalf("Delete: err = %v, want ErrGoalNotFound", err)
	}
	if len(repo.deleted) != 0 || repo.goals[g.ID].Name != "Retirement" {
		t.Fatal("another user changed alice's goal")
	}
}

func TestUpdate_ReplacesFieldsAndUnlinks(t *testing.T) {
	uc, _ := setup(50)
	ctx := context.Background()
	in := projection()
	in.PortfolioID = str("pf-alice")
	g, err := uc.Create(ctx, alice, in)
	if err != nil {
		t.Fatal(err)
	}

	replacement := &goaldomain.Goal{
		Name: "House down payment", Kind: goaldomain.KindTarget, TargetAmount: f64(1_000_000), Years: 5,
		ExpectedReturn: 4, Currency: "THB",
	}
	updated, err := uc.Update(ctx, alice, g.ID, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != g.ID || updated.Name != "House down payment" || updated.Kind != goaldomain.KindTarget ||
		updated.PortfolioID != nil || updated.Currency != "THB" {
		t.Fatalf("unexpected update: %+v", updated)
	}

	got, err := uc.Get(ctx, alice, g.ID)
	if err != nil || got.Name != "House down payment" {
		t.Fatalf("Get after update: %+v, %v", got, err)
	}
}

func TestUpdate_RejectsForeignPortfolio(t *testing.T) {
	uc, _ := setup(50)
	ctx := context.Background()
	g, err := uc.Create(ctx, alice, projection())
	if err != nil {
		t.Fatal(err)
	}
	in := projection()
	in.PortfolioID = str("pf-bob")
	if _, err := uc.Update(ctx, alice, g.ID, in); !errors.Is(err, goaldomain.ErrInvalidPortfolio) {
		t.Fatalf("err = %v, want ErrInvalidPortfolio", err)
	}
}

func TestDelete_Own(t *testing.T) {
	uc, _ := setup(50)
	ctx := context.Background()
	g, err := uc.Create(ctx, alice, projection())
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Delete(ctx, alice, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Get(ctx, alice, g.ID); !errors.Is(err, goaldomain.ErrGoalNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrGoalNotFound", err)
	}
}

func TestUnknownUser(t *testing.T) {
	uc, _ := setup(50)
	if _, err := uc.Create(context.Background(), "nobody@example.com", projection()); !errors.Is(err, userdomain.ErrNotFound) {
		t.Fatalf("err = %v, want wrapped user ErrNotFound", err)
	}
}
