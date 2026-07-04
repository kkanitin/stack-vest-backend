package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
)

type visitorLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func newVisitorLimiter(r rate.Limit, b int) *visitorLimiter {
	return &visitorLimiter{limiters: make(map[string]*rate.Limiter), r: r, b: b}
}

func (v *visitorLimiter) get(key string) *rate.Limiter {
	v.mu.Lock()
	defer v.mu.Unlock()
	l, ok := v.limiters[key]
	if !ok {
		l = rate.NewLimiter(v.r, v.b)
		v.limiters[key] = l
	}
	return l
}

// RateLimit throttles requests per caller (authenticated user if available, else
// client IP) using a token bucket, to bound cost-amplification against paid
// FMP/Groq quotas. Known limitation: the per-key limiter map has no eviction and
// grows unboundedly over the process lifetime — acceptable for now; revisit if
// memory becomes a concern.
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	vl := newVisitorLimiter(rate.Limit(rps), burst)
	return func(c *gin.Context) {
		key := c.GetString(UserIDKey)
		if key == "" {
			key = c.ClientIP()
		}
		if !vl.get(key).Allow() {
			response.Err(c, http.StatusTooManyRequests, "rate limit exceeded, slow down")
			c.Abort()
			return
		}
		c.Next()
	}
}
