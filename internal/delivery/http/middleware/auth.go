package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid token"})
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		payload, err := idtoken.Validate(c.Request.Context(), tokenStr, googleClientID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
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
