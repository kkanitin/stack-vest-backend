package middleware

import (
	"compress/gzip"
	"strings"

	"github.com/gin-gonic/gin"
)

type gzipWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
}

func (w *gzipWriter) Write(b []byte) (int, error) { return w.writer.Write(b) }

func (w *gzipWriter) WriteString(s string) (int, error) { return w.writer.Write([]byte(s)) }

// Gzip compresses responses when the client advertises support via
// Accept-Encoding. Both SSE routes end in "/analyze" and no other route does
// (POST /api/v1/portfolios/analyze and POST /api/v1/portfolios/:id/analyze) —
// matched by suffix rather than exact path, since the second is parameterized
// and wouldn't match a literal exclusion list at the real request path (e.g.
// /api/v1/portfolios/abc123/analyze). SSE must never be wrapped: gzip buffers
// writes internally, which would delay or coalesce Server-Sent Event frames.
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") ||
			strings.HasSuffix(c.Request.URL.Path, "/analyze") {
			c.Next()
			return
		}

		original := c.Writer
		gz := gzip.NewWriter(original)
		defer gz.Close()

		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")
		c.Writer = &gzipWriter{ResponseWriter: original, writer: gz}
		c.Next()
		c.Writer.Header().Del("Content-Length")
	}
}
