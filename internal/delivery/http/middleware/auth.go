package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
)

const (
	UserIDKey  = "userID"
	EmailKey   = "email"
	NameKey    = "name"
	PictureKey = "picture"
)

func Auth(googleClientID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.Err(c, http.StatusUnauthorized, "missing or invalid token")
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		payload, err := idtoken.Validate(c.Request.Context(), tokenStr, googleClientID)
		if err != nil {
			response.Err(c, http.StatusUnauthorized, "invalid token")
			c.Abort()
			return
		}

		if !boolClaim(payload.Claims, "email_verified") {
			response.Err(c, http.StatusUnauthorized, "email not verified")
			c.Abort()
			return
		}

		c.Set(UserIDKey, stringClaim(payload.Claims, "sub"))
		c.Set(EmailKey, stringClaim(payload.Claims, "email"))
		c.Set(NameKey, stringClaim(payload.Claims, "name"))
		c.Set(PictureKey, stringClaim(payload.Claims, "picture"))
		c.Next()
	}
}

func stringClaim(claims map[string]interface{}, key string) string {
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func boolClaim(claims map[string]interface{}, key string) bool {
	if v, ok := claims[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
