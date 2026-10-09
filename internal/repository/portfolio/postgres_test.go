package portfolio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsMalformedID(t *testing.T) {
	invalidUUID := &pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "nope"`}
	if !isMalformedID(invalidUUID) {
		t.Fatal("22P02 must be treated as a malformed id")
	}
	if !isMalformedID(fmt.Errorf("scan: %w", invalidUUID)) {
		t.Fatal("a wrapped 22P02 must be treated as a malformed id")
	}
	if isMalformedID(&pgconn.PgError{Code: "23505"}) || isMalformedID(errors.New("boom")) || isMalformedID(nil) {
		t.Fatal("other errors must not be treated as a malformed id")
	}
}
