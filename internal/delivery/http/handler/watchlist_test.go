package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	watchlistdomain "github.com/kanitin/stackvest/backend/internal/domain/watchlist"
	watchlistuc "github.com/kanitin/stackvest/backend/internal/usecase/watchlist"
)

type stubWatchlistRepo struct {
	items []watchlistdomain.Item
}

func (m *stubWatchlistRepo) Add(_ context.Context, item *watchlistdomain.Item) (*watchlistdomain.Item, error) {
	return item, nil
}
func (m *stubWatchlistRepo) Remove(_ context.Context, _, _ string) error { return nil }
func (m *stubWatchlistRepo) ListByUserID(_ context.Context, _ string) ([]watchlistdomain.Item, error) {
	return m.items, nil
}
func (m *stubWatchlistRepo) SetAlertsEnabled(_ context.Context, _, _ string, _ bool) (*watchlistdomain.Item, error) {
	return nil, nil
}

type stubWatchlistUserRepo struct{}

func (m *stubWatchlistUserRepo) FindByEmail(_ context.Context, _ string) (*userdomain.User, error) {
	return &userdomain.User{ID: "u1"}, nil
}
func (m *stubWatchlistUserRepo) FindByGoogleID(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (m *stubWatchlistUserRepo) Upsert(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}
func (m *stubWatchlistUserRepo) Create(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}

type stubWatchlistSearcher struct{}

func (m *stubWatchlistSearcher) SearchSymbol(_ string) ([]stockdomain.Match, error) { return nil, nil }
func (m *stubWatchlistSearcher) SearchName(_ string) ([]stockdomain.Match, error)   { return nil, nil }

func makeWatchlistItems(n int) []watchlistdomain.Item {
	items := make([]watchlistdomain.Item, n)
	for i := range items {
		items[i].Symbol = fmt.Sprintf("SYM%03d", i)
	}
	return items
}

func newWatchlistRouter(items []watchlistdomain.Item) *gin.Engine {
	gin.SetMode(gin.TestMode)
	uc := watchlistuc.NewWatchlistUseCase(&stubWatchlistRepo{items: items}, &stubWatchlistUserRepo{}, &stubWatchlistSearcher{})
	r := gin.New()
	NewWatchlistHandler(uc).RegisterRoutes(r.Group(""))
	return r
}

type watchlistListResponse struct {
	Meta struct {
		Total            *int `json:"total"`
		Page             *int `json:"page"`
		Size             *int `json:"size"`
		CurrentPageCount *int `json:"currentPageCount"`
	} `json:"meta"`
	Results []watchlistdomain.Item `json:"results"`
}

func TestWatchlistList_Pagination(t *testing.T) {
	r := newWatchlistRouter(makeWatchlistItems(7))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/watchlist?page=2&size=3", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp watchlistListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results on page 2, got %d", len(resp.Results))
	}
	if resp.Results[0].Symbol != "SYM003" {
		t.Fatalf("expected page 2 to start at SYM003, got %s", resp.Results[0].Symbol)
	}
	if *resp.Meta.Total != 7 {
		t.Fatalf("expected total 7, got %d", *resp.Meta.Total)
	}
	if *resp.Meta.Page != 2 || *resp.Meta.Size != 3 || *resp.Meta.CurrentPageCount != 3 {
		t.Fatalf("unexpected meta: %+v", resp.Meta)
	}
}

func TestWatchlistList_PageBeyondEndReturnsEmptyWithCorrectTotal(t *testing.T) {
	r := newWatchlistRouter(makeWatchlistItems(3))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/watchlist?page=5&size=10", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp watchlistListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("expected 0 results past the end, got %d", len(resp.Results))
	}
	if *resp.Meta.Total != 3 {
		t.Fatalf("expected total to stay 3 even past the last page, got %d", *resp.Meta.Total)
	}
}

func TestWatchlistList_InvalidPageRejected(t *testing.T) {
	r := newWatchlistRouter(nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/watchlist?page=0", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
