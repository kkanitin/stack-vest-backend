package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
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

// googleLogin redirects the browser to the Google OAuth consent page.
func (h *AuthHandler) googleLogin(c *gin.Context) {
	// TODO: replace with a per-request random state stored in a short-lived cookie for CSRF protection
	state := "stackvest-oauth-state"
	c.Redirect(http.StatusTemporaryRedirect, h.googleUC.GetAuthURL(state))
}

// googleCallback handles the redirect from Google, exchanges the code for a JWT.
func (h *AuthHandler) googleCallback(c *gin.Context) {
	// TODO: verify state matches the value sent in googleLogin
	code := c.Query("code")
	if code == "" {
		response.Err(c, http.StatusBadRequest, "missing code")
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
