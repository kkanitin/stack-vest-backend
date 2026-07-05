package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAuth_MissingToken exercises only the pre-idtoken.Validate path (no
// Authorization header), since idtoken.Validate calls Google's real JWKS
// endpoint and can't be exercised in a unit test without a fake token issuer.
// This is enough to pin issue #10 (response envelope) and the abort behavior
// that issues #4/#10 both depend on.
func TestAuth_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handlerCalled := false
	r.Use(Auth("test-client-id"))
	r.GET("/protected", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if handlerCalled {
		t.Fatal("expected chain to abort before reaching the protected handler")
	}

	var body struct {
		ErrorMessage *string `json:"errorMessage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ErrorMessage == nil {
		t.Fatal("expected errorMessage to be populated via the standard response envelope")
	}
}

func TestBoolClaim(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]interface{}
		want   bool
	}{
		{"true", map[string]interface{}{"email_verified": true}, true},
		{"false", map[string]interface{}{"email_verified": false}, false},
		{"missing", map[string]interface{}{}, false},
		{"wrong type", map[string]interface{}{"email_verified": "true"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := boolClaim(tc.claims, "email_verified"); got != tc.want {
				t.Errorf("boolClaim: want %v, got %v", tc.want, got)
			}
		})
	}
}
