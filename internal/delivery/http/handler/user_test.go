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

func TestGetMe(t *testing.T) {
	tests := []struct {
		name        string
		findByEmail func(context.Context, string) (*userdomain.User, error)
		wantCode    int
	}{
		{"not found", func(_ context.Context, _ string) (*userdomain.User, error) {
			return nil, userdomain.ErrNotFound
		}, http.StatusNotFound},
		{"internal error", func(_ context.Context, _ string) (*userdomain.User, error) {
			return nil, context.DeadlineExceeded
		}, http.StatusInternalServerError},
		{"success", func(_ context.Context, _ string) (*userdomain.User, error) {
			return &userdomain.User{ID: "123", Email: "test@example.com", Name: "Test User"}, nil
		}, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newUserRouter(&mockUserRepo{findByEmailFn: tc.findByEmail})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}

func TestCreateMe(t *testing.T) {
	tests := []struct {
		name     string
		createFn func(context.Context, *userdomain.User) (*userdomain.User, error)
		wantCode int
	}{
		{"already exists", func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return nil, userdomain.ErrAlreadyExists
		}, http.StatusConflict},
		{"internal error", func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return nil, context.DeadlineExceeded
		}, http.StatusInternalServerError},
		{"success", func(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
			return &userdomain.User{ID: "123", Email: "test@example.com", Name: "Test User"}, nil
		}, http.StatusCreated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newUserRouter(&mockUserRepo{createFn: tc.createFn})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users/me", nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}
