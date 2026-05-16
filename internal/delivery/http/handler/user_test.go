package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	useruc "github.com/kanitin/stackvest/backend/internal/usecase/user"
)

type mockUserRepo struct {
	findByEmailFn func(ctx context.Context, email string) (*userdomain.User, error)
	createFn      func(ctx context.Context, user *userdomain.User) (*userdomain.User, error)
}

func (m *mockUserRepo) FindByEmail(ctx context.Context, email string) (*userdomain.User, error) {
	if m.findByEmailFn != nil {
		return m.findByEmailFn(ctx, email)
	}
	return nil, nil
}

func (m *mockUserRepo) Create(ctx context.Context, user *userdomain.User) (*userdomain.User, error) {
	if m.createFn != nil {
		return m.createFn(ctx, user)
	}
	return nil, nil
}

func (m *mockUserRepo) FindByGoogleID(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}

func (m *mockUserRepo) Upsert(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}

func newUserRouter(repo userdomain.Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(
		func(c *gin.Context) {
			c.Set(middleware.EmailKey, "test@example.com")
			c.Set(middleware.NameKey, "Test User")
			c.Set(middleware.PictureKey, "https://example.com/pic.jpg")
			c.Next()
		},
	)
	NewUserHandler(useruc.NewUserUseCase(repo)).RegisterRoutes(r.Group(""))
	return r
}

func TestGetMe_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		findByEmailFn: func(_ context.Context, _ string) (*userdomain.User, error) {
			return nil, userdomain.ErrNotFound
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetMe_InternalError(t *testing.T) {
	repo := &mockUserRepo{
		findByEmailFn: func(_ context.Context, _ string) (*userdomain.User, error) {
			return nil, context.DeadlineExceeded
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetMe_Success(t *testing.T) {
	user := &userdomain.User{ID: "123", Email: "test@example.com", Name: "Test User"}
	repo := &mockUserRepo{
		findByEmailFn: func(_ context.Context, _ string) (*userdomain.User, error) {
			return user, nil
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCreateMe_AlreadyExists(t *testing.T) {
	repo := &mockUserRepo{
		createFn: func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return nil, userdomain.ErrAlreadyExists
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users/me", nil))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateMe_InternalError(t *testing.T) {
	repo := &mockUserRepo{
		createFn: func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return nil, context.DeadlineExceeded
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users/me", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestCreateMe_Success(t *testing.T) {
	user := &userdomain.User{ID: "123", Email: "test@example.com", Name: "Test User"}
	repo := &mockUserRepo{
		createFn: func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return user, nil
		},
	}
	w := httptest.NewRecorder()
	newUserRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users/me", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}
