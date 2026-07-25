package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
)

// stubUserRepo satisfies userdomain.Repository for auth tests; no methods are called
// during the paths exercised here (login redirect, missing-code callback).
type stubUserRepo struct{}

func (s *stubUserRepo) FindByGoogleID(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (s *stubUserRepo) FindByEmail(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (s *stubUserRepo) Upsert(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}
func (s *stubUserRepo) Create(_ context.Context, _ *userdomain.User) (*userdomain.User, error) {
	return nil, nil
}

func newAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	uc := authuc.NewGoogleUseCase("client-id", "client-secret", "http://localhost/callback", &stubUserRepo{})
	r := gin.New()
	NewAuthHandler(uc, "test-secret").RegisterRoutes(r.Group(""))
	return r
}

func TestGoogleLogin_Redirect(t *testing.T) {
	w := httptest.NewRecorder()
	newAuthRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/google", nil))

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "https://accounts.google.com") {
		t.Fatalf("expected redirect to accounts.google.com, got %s", location)
	}
}

func TestGoogleCallback_MissingCode(t *testing.T) {
	w := httptest.NewRecorder()
	newAuthRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGoogleLogin_SetsStateCookie(t *testing.T) {
	w := httptest.NewRecorder()
	newAuthRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/google", nil))

	var stateCookie *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == oauthStateCookie {
			stateCookie = ck
		}
	}
	if stateCookie == nil {
		t.Fatal("expected oauth_state cookie to be set")
	}
	if stateCookie.Value == "" {
		t.Error("expected non-empty state cookie value")
	}
	if !stateCookie.HttpOnly {
		t.Error("expected state cookie to be HttpOnly")
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "state="+stateCookie.Value) {
		t.Errorf("expected redirect URL state param to match cookie value, got location=%s cookie=%s", location, stateCookie.Value)
	}
}

func TestGoogleCallback_StateMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=attacker-controlled", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: "victim-real-state"})

	w := httptest.NewRecorder()
	newAuthRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on state mismatch, got %d", w.Code)
	}
}

func TestGoogleCallback_MissingStateCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=abc&state=some-state", nil)

	w := httptest.NewRecorder()
	newAuthRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no state cookie was set, got %d", w.Code)
	}
}
