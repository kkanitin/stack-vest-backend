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

func TestHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// pgxpool.New parses config and constructs the pool without dialing — no
	// connection is made until something acquires one, so this is safe to use
	// against an unreachable DSN in a unit test. Stat() reports zero-valued
	// pool stats.
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db")
	if err != nil {
		t.Fatalf("failed to construct pool: %v", err)
	}
	defer pool.Close()

	r := gin.New()
	r.GET("/health", NewHealthHandler(pool).HealthCheck)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Message string `json:"message"`
		DBPool  struct {
			MaxConns int32 `json:"maxConns"`
		} `json:"dbPool"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Message != "ready" {
		t.Fatalf("expected message %q, got %q", "ready", body.Message)
	}
	if body.DBPool.MaxConns <= 0 {
		t.Fatalf("expected a positive maxConns, got %d", body.DBPool.MaxConns)
	}
}
