package dividend

import (
	"errors"
	"testing"
	"time"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
)

// day parses a fixed YYYY-MM-DD test date; the empty string is the zero time (an
// omitted from/to).
func day(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestResolveWindow(t *testing.T) {
	tests := []struct {
		name     string
		today    string
		from, to string
		wantFrom string
		wantTo   string
		wantErr  error
	}{
		{"defaults", "2026-10-01", "", "", "2026-10-01", "2026-12-15", nil},
		{"only from", "2026-10-01", "2026-11-10", "", "2026-11-10", "2027-01-24", nil},
		{"only to", "2026-10-01", "", "2026-10-20", "2026-10-01", "2026-10-20", nil},
		{"past month accepted unchanged", "2026-10-01", "2026-08-01", "2026-08-31", "2026-08-01", "2026-08-31", nil},
		{"single day", "2026-10-01", "2026-10-05", "2026-10-05", "2026-10-05", "2026-10-05", nil},

		{"to before from", "2026-10-01", "2026-10-10", "2026-10-09", "", "", dividenddomain.ErrInvalidRange},
		{"only to, before today", "2026-10-01", "", "2026-09-15", "", "", dividenddomain.ErrInvalidRange},

		{"span of 92 days", "2026-10-01", "2026-10-01", "2027-01-01", "2026-10-01", "2027-01-01", nil},
		{"span of 93 days", "2026-10-01", "2026-10-01", "2027-01-02", "", "", dividenddomain.ErrRangeTooLong},

		{"lower bound", "2026-10-01", "2025-09-01", "2025-09-30", "2025-09-01", "2025-09-30", nil},
		{"before lower bound", "2026-10-01", "2025-08-31", "2025-09-30", "", "", dividenddomain.ErrRangeOutOfBounds},
		{"upper bound", "2026-10-01", "2027-11-01", "2027-11-30", "2027-11-01", "2027-11-30", nil},
		{"after upper bound", "2026-10-01", "2027-11-01", "2027-12-01", "", "", dividenddomain.ErrRangeOutOfBounds},
		// A defaulted to must not push an in-bounds from out of bounds.
		{"only from, default to capped at upper bound", "2026-10-01", "2027-10-01", "", "2027-10-01", "2027-11-30", nil},
		{"only from, itself after upper bound", "2026-10-01", "2027-12-05", "", "", "", dividenddomain.ErrRangeOutOfBounds},

		// today on the 31st: AddDate(0, ±13, 0) on that day would overflow into the
		// following month (2025-09-31 → 2025-10-01, 2027-11-31 → 2027-12-01) and shift
		// both bounds. They must match the 2026-10-01 cases above.
		{"day 31, lower bound", "2026-10-31", "2025-09-01", "2025-09-30", "2025-09-01", "2025-09-30", nil},
		{"day 31, before lower bound", "2026-10-31", "2025-08-31", "2025-09-30", "", "", dividenddomain.ErrRangeOutOfBounds},
		{"day 31, upper bound", "2026-10-31", "2027-11-01", "2027-11-30", "2027-11-01", "2027-11-30", nil},
		{"day 31, after upper bound", "2026-10-31", "2027-11-01", "2027-12-01", "", "", dividenddomain.ErrRangeOutOfBounds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to, err := resolveWindow(day(tc.today), day(tc.from), day(tc.to))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !from.Equal(day(tc.wantFrom)) || !to.Equal(day(tc.wantTo)) {
				t.Errorf("window: want [%s, %s], got [%s, %s]",
					tc.wantFrom, tc.wantTo, from.Format("2006-01-02"), to.Format("2006-01-02"))
			}
		})
	}
}

func TestMonthsIn(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		want     []string
	}{
		{"single day", "2026-10-15", "2026-10-15", []string{"2026-10-01"}},
		{"whole month", "2026-10-01", "2026-10-31", []string{"2026-10-01"}},
		{
			"from on day 31", "2027-01-31", "2027-05-03",
			[]string{"2027-01-01", "2027-02-01", "2027-03-01", "2027-04-01", "2027-05-01"},
		},
		{"year rollover", "2026-12-15", "2027-01-10", []string{"2026-12-01", "2027-01-01"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := monthsIn(day(tc.from), day(tc.to))
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d months, got %d: %v", len(tc.want), len(got), got)
			}
			for i, m := range got {
				if !m.Equal(day(tc.want[i])) {
					t.Errorf("month %d: want %s, got %s", i, tc.want[i], m.Format("2006-01-02"))
				}
			}
		})
	}
}

func TestBucketKey(t *testing.T) {
	if got := bucketKey(day("2026-10-01")); got != "calendar:2026-10" {
		t.Errorf("bucket key: want calendar:2026-10, got %s", got)
	}
}
