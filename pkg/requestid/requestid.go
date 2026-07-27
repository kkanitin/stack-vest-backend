// Package requestid generates and propagates a per-request correlation ID
// via context.Context, independent of any logging library.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey struct{}

// New generates a random request ID.
func New() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewContext returns a copy of ctx carrying the given request ID.
func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the request ID stored in ctx, or "" if absent.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}
