package portfolio

import (
	"testing"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

func pts(dates ...string) []*portfoliodomain.ValuePoint {
	out := make([]*portfoliodomain.ValuePoint, len(dates))
	for i, d := range dates {
		out[i] = &portfoliodomain.ValuePoint{Date: d, Value: float64(i + 1)}
	}
	return out
}

func TestAlignBenchmark(t *testing.T) {
	const none = -1.0 // marks "BenchmarkClose must be nil"
	tests := []struct {
		name   string
		points []*portfoliodomain.ValuePoint
		closes []stockdomain.HistoryPoint
		want   []float64
	}{
		{
			// 2026-09-12 is a Saturday: the first day uses Friday's close.
			name:   "weekend first day uses the previous close",
			points: pts("2026-09-12", "2026-09-13", "2026-09-14"),
			closes: []stockdomain.HistoryPoint{{Date: "2026-09-10", Close: 100}, {Date: "2026-09-11", Close: 101}, {Date: "2026-09-14", Close: 103}},
			want:   []float64{101, 101, 103},
		},
		{
			name:   "exact match wins over an earlier close",
			points: pts("2026-09-09", "2026-09-10"),
			closes: []stockdomain.HistoryPoint{{Date: "2026-09-08", Close: 1}, {Date: "2026-09-09", Close: 2}, {Date: "2026-09-10", Close: 3}},
			want:   []float64{2, 3},
		},
		{
			name:   "leading days with no close stay nil",
			points: pts("2026-09-01", "2026-09-02", "2026-09-03"),
			closes: []stockdomain.HistoryPoint{{Date: "2026-09-02", Close: 50}},
			want:   []float64{none, 50, 50},
		},
		{
			name:   "closes after the last point are ignored",
			points: pts("2026-09-01", "2026-09-02"),
			closes: []stockdomain.HistoryPoint{{Date: "2026-08-31", Close: 9}, {Date: "2026-09-05", Close: 99}},
			want:   []float64{9, 9},
		},
		{
			name:   "empty closes",
			points: pts("2026-09-01", "2026-09-02"),
			closes: nil,
			want:   []float64{none, none},
		},
		{
			name:   "no points",
			points: nil,
			closes: []stockdomain.HistoryPoint{{Date: "2026-09-01", Close: 1}},
			want:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			alignBenchmark(tc.points, tc.closes)
			if len(tc.points) != len(tc.want) {
				t.Fatalf("point count changed: %d", len(tc.points))
			}
			for i, p := range tc.points {
				switch {
				case tc.want[i] == none && p.BenchmarkClose != nil:
					t.Fatalf("point %d (%s): expected nil, got %v", i, p.Date, *p.BenchmarkClose)
				case tc.want[i] != none && (p.BenchmarkClose == nil || *p.BenchmarkClose != tc.want[i]):
					t.Fatalf("point %d (%s): expected %v, got %v", i, p.Date, tc.want[i], p.BenchmarkClose)
				}
			}
		})
	}
}

func TestAlignBenchmark_PointersDoNotAlias(t *testing.T) {
	points := pts("2026-09-12", "2026-09-13")
	alignBenchmark(points, []stockdomain.HistoryPoint{{Date: "2026-09-11", Close: 1}})
	*points[0].BenchmarkClose = 42
	if *points[1].BenchmarkClose != 1 {
		t.Fatal("forward-filled points must not share one float")
	}
}
