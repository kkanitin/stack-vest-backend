package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kanitin/stackvest/backend/pkg/requestid"
)

// Logger returns a Gin middleware that emits a structured log line per request.
func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := requestid.New()
		c.Request = c.Request.WithContext(requestid.NewContext(c.Request.Context(), id))
		c.Writer.Header().Set("X-Request-ID", id)

		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("request_id", id),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
			zap.Int("size", c.Writer.Size()),
		}
		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		lvl := zapcore.InfoLevel
		switch {
		case status >= 500:
			lvl = zapcore.ErrorLevel
		case status >= 400:
			lvl = zapcore.WarnLevel
		}

		// Check+Write (rather than a switch across log.Info/Warn/Error) avoids
		// building the field slice three different ways for one runtime-computed level.
		if ce := log.Check(lvl, "request"); ce != nil {
			ce.Write(fields...)
		}
	}
}
