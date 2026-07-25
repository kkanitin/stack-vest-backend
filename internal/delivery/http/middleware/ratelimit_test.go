package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimit_AllowsBurstThenRejects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(1, 2)) // burst of 2, refills slowly
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	codes := make([]int, 3)
	for i := range codes {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		codes[i] = w.Code
	}

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("expected first 2 requests (burst) to succeed, got %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("expected 3rd request to be rate limited, got %d", codes[2])
	}
}

func TestRateLimit_KeysByUserIDWhenSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(UserIDKey, c.GetHeader("X-Test-User"))
		c.Next()
	})
	r.Use(RateLimit(1, 1))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Exhaust user "a"'s single-token bucket.
	reqA1 := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqA1.Header.Set("X-Test-User", "a")
	wA1 := httptest.NewRecorder()
	r.ServeHTTP(wA1, reqA1)
	if wA1.Code != http.StatusOK {
		t.Fatalf("expected user a's first request to succeed, got %d", wA1.Code)
	}

	reqA2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqA2.Header.Set("X-Test-User", "a")
	wA2 := httptest.NewRecorder()
	r.ServeHTTP(wA2, reqA2)
	if wA2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected user a's second request to be limited, got %d", wA2.Code)
	}

	// A different user must have an independent bucket.
	reqB := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqB.Header.Set("X-Test-User", "b")
	wB := httptest.NewRecorder()
	r.ServeHTTP(wB, reqB)
	if wB.Code != http.StatusOK {
		t.Fatalf("expected user b's request to succeed independently, got %d", wB.Code)
	}
}
