package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool constructs a pool without dialing — pgxpool.New makes no
// connection until something acquires one, so this is safe to use against an
// unreachable DSN in a unit test. Stat() reports zero-valued pool stats.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db")
	if err != nil {
		t.Fatalf("failed to construct pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/health", NewHealthHandler(newTestPool(t)).HealthCheck)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["message"] != "ready" {
		t.Fatalf("expected message %q, got %v", "ready", body["message"])
	}
	if len(body) != 1 {
		t.Fatalf("expected only the message field on the public probe, got %v", body)
	}
}

func TestDBPoolStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	NewHealthHandler(newTestPool(t)).RegisterRoutes(r.Group(""))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/db", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Result struct {
			MaxConns int32 `json:"maxConns"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Result.MaxConns <= 0 {
		t.Fatalf("expected a positive maxConns, got %d", body.Result.MaxConns)
	}
}
