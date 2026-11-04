package goal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
)

func TestIsMalformedID(t *testing.T) {
	invalid := &pgconn.PgError{Code: "22P02"}
	if !isMalformedID(invalid) || !isMalformedID(fmt.Errorf("scan: %w", invalid)) {
		t.Fatal("22P02 (wrapped or not) must be treated as a malformed id")
	}
	if isMalformedID(&pgconn.PgError{Code: "23505"}) || isMalformedID(errors.New("boom")) || isMalformedID(nil) {
		t.Fatal("other errors must not be treated as a malformed id")
	}
}

func TestMapWriteErr(t *testing.T) {
	if !errors.Is(mapWriteErr(&pgconn.PgError{Code: "23503"}), goaldomain.ErrInvalidPortfolio) {
		t.Fatal("a foreign-key violation must map to ErrInvalidPortfolio")
	}
	if !errors.Is(mapWriteErr(&pgconn.PgError{Code: "22P02"}), goaldomain.ErrInvalidPortfolio) {
		t.Fatal("a malformed portfolio id must map to ErrInvalidPortfolio")
	}
	other := errors.New("boom")
	if mapWriteErr(other) != other {
		t.Fatal("other errors must pass through")
	}
}
