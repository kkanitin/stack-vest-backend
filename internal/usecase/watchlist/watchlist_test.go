package watchlist_test

import (
	"context"
	"testing"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	watchlistdomain "github.com/kanitin/stackvest/backend/internal/domain/watchlist"
	watchlistuc "github.com/kanitin/stackvest/backend/internal/usecase/watchlist"
)

type mockRepo struct {
	addedItem *watchlistdomain.Item
}

func (m *mockRepo) Add(_ context.Context, item *watchlistdomain.Item) (*watchlistdomain.Item, error) {
	m.addedItem = item
	return item, nil
}
func (m *mockRepo) Remove(_ context.Context, _, _ string) error { return nil }
func (m *mockRepo) ListByUserID(_ context.Context, _ string) ([]watchlistdomain.Item, error) {
	return nil, nil
}
func (m *mockRepo) SetAlertsEnabled(_ context.Context, _, _ string, _ bool) (*watchlistdomain.Item, error) {
	return nil, nil
}

var _ watchlistdomain.Repository = (*mockRepo)(nil)

type mockUserRepo struct{ user *userdomain.User }

func (m *mockUserRepo) FindByEmail(_ context.Context, _ string) (*userdomain.User, error) {
	return m.user, nil
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

var _ userdomain.Repository = (*mockUserRepo)(nil)

type mockSearcher struct{ matches []stockdomain.Match }

func (m *mockSearcher) SearchSymbol(_ string) ([]stockdomain.Match, error) { return m.matches, nil }
func (m *mockSearcher) SearchName(_ string) ([]stockdomain.Match, error)   { return m.matches, nil }

var _ stockdomain.Searcher = (*mockSearcher)(nil)

func TestAdd_UsesClientSuppliedNameAlways(t *testing.T) {
	repo := &mockRepo{}
	searcher := &mockSearcher{matches: []stockdomain.Match{{Symbol: "AAPL", Name: "Apple Inc FMP Name", Type: "stock"}}}
	uc := watchlistuc.NewWatchlistUseCase(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, searcher)

	_, err := uc.Add(context.Background(), "a@b.com", "AAPL", "My Custom Name", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.addedItem.Name != "My Custom Name" {
		t.Fatalf("expected client-supplied name to be kept, got %q", repo.addedItem.Name)
	}
}

func TestAdd_TypeFallsBackToFMPWhenOmitted(t *testing.T) {
	repo := &mockRepo{}
	searcher := &mockSearcher{matches: []stockdomain.Match{{Symbol: "AAPL", Name: "Apple Inc", Type: "stock"}}}
	uc := watchlistuc.NewWatchlistUseCase(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, searcher)

	_, err := uc.Add(context.Background(), "a@b.com", "AAPL", "Apple", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.addedItem.Type != "stock" {
		t.Fatalf("expected type to fall back to FMP's value, got %q", repo.addedItem.Type)
	}
}

func TestAdd_TypeUsesClientValueWhenSupplied(t *testing.T) {
	repo := &mockRepo{}
	searcher := &mockSearcher{matches: []stockdomain.Match{{Symbol: "AAPL", Name: "Apple Inc", Type: "stock"}}}
	uc := watchlistuc.NewWatchlistUseCase(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, searcher)

	_, err := uc.Add(context.Background(), "a@b.com", "AAPL", "Apple", "etf", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.addedItem.Type != "etf" {
		t.Fatalf("expected client-supplied type to be kept, got %q", repo.addedItem.Type)
	}
}
