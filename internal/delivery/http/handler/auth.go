package handler

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
)

const (
	oauthStateCookie = "oauth_state"
	oauthStateMaxAge = 10 * 60 // seconds
)

type AuthHandler struct {
	googleUC  *authuc.GoogleUseCase
	jwtSecret string
}

func NewAuthHandler(googleUC *authuc.GoogleUseCase, jwtSecret string) *AuthHandler {
	return &AuthHandler{googleUC: googleUC, jwtSecret: jwtSecret}
}

func (h *AuthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	auth.GET("/google", h.googleLogin)
	auth.GET("/google/callback", h.googleCallback)
}

// googleLogin redirects the browser to the Google OAuth consent page, carrying a
// per-request random state nonce that googleCallback verifies against a matching
// short-lived cookie to prevent OAuth login CSRF.
func (h *AuthHandler) googleLogin(c *gin.Context) {
	state, err := generateState()
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to generate oauth state", "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to start login")
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, state, oauthStateMaxAge, "/", "", isSecureRequest(c), true)

	c.Redirect(http.StatusTemporaryRedirect, h.googleUC.GetAuthURL(state))
}

// googleCallback handles the redirect from Google, exchanges the code for a JWT.
func (h *AuthHandler) googleCallback(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		response.Err(c, http.StatusBadRequest, "missing code")
		return
	}

	returnedState := c.Query("state")
	cookieState, cookieErr := c.Cookie(oauthStateCookie)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, "", -1, "/", "", isSecureRequest(c), true) // clear single-use cookie

	if cookieErr != nil || returnedState == "" || returnedState != cookieState {
		response.Err(c, http.StatusBadRequest, "invalid or missing oauth state")
		return
	}

	user, err := h.googleUC.HandleCallback(c.Request.Context(), code)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "google callback failed", "error", err)
		response.Err(c, http.StatusInternalServerError, err.Error())
		return
	}

	claims := jwt.MapClaims{
		"sub":     user.ID,
		"email":   user.Email,
		"name":    user.Name,
		"picture": user.Picture,
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.jwtSecret))
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to sign JWT", "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to sign token")
		return
	}

	response.OK(c, gin.H{"token": signed, "user": user})
}

// generateState returns a URL-safe random nonce for CSRF protection on the OAuth flow.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// isSecureRequest is a best-effort check for whether the oauth state cookie should
// carry the Secure flag. Behind a TLS-terminating reverse proxy this reads false
// unless X-Forwarded-Proto is also checked — acceptable for a short-lived,
// single-use CSRF nonce.
func isSecureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil
}
