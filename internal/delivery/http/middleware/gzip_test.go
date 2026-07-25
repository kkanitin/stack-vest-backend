package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newGzipTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	body := strings.Repeat("stackvest-performance-review-payload ", 200) // compressible
	r.GET("/api/v1/stocks/history", func(c *gin.Context) {
		c.String(http.StatusOK, body)
	})
	r.POST("/api/v1/portfolios/:id/analyze", func(c *gin.Context) {
		c.String(http.StatusOK, body)
	})
	r.POST("/api/v1/portfolios/analyze", func(c *gin.Context) {
		c.String(http.StatusOK, body)
	})
	return r
}

func TestGzip_CompressesWhenAccepted(t *testing.T) {
	r := newGzipTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/history", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", enc)
	}
	if w.Header().Get("Content-Length") != "" {
		t.Fatal("expected Content-Length to be removed after compression")
	}

	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("response body is not valid gzip: %v", err)
	}
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("failed to decode gzip body: %v", err)
	}
	if !strings.Contains(string(decoded), "stackvest-performance-review-payload") {
		t.Fatalf("decoded body missing expected content: %q", string(decoded)[:min(80, len(decoded))])
	}
}

func TestGzip_SkipsWithoutAcceptEncoding(t *testing.T) {
	r := newGzipTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("expected no Content-Encoding without Accept-Encoding, got %q", enc)
	}
	if !strings.Contains(w.Body.String(), "stackvest-performance-review-payload") {
		t.Fatal("expected uncompressed plaintext body")
	}
}

func TestGzip_ExcludesParameterizedAnalyzeRoute(t *testing.T) {
	r := newGzipTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/portfolios/abc123/analyze", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("expected the parameterized /analyze route to be excluded from gzip, got Content-Encoding %q", enc)
	}
	if !strings.Contains(w.Body.String(), "stackvest-performance-review-payload") {
		t.Fatal("expected uncompressed plaintext body on the excluded SSE route")
	}
}

func TestGzip_ExcludesUnparameterizedAnalyzeRoute(t *testing.T) {
	r := newGzipTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/portfolios/analyze", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("expected the /analyze route to be excluded from gzip, got Content-Encoding %q", enc)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
