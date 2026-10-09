package portfolio

import (
	"reflect"
	"testing"
)

func TestParseBenchmarks(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []Benchmark
	}{
		{"default style", []string{"SPY=S&P 500", "QQQ=Nasdaq 100"}, []Benchmark{{"SPY", "S&P 500"}, {"QQQ", "Nasdaq 100"}}},
		{"trims spaces left by env split", []string{" spy = S&P 500 ", " vt=Total world"}, []Benchmark{{"SPY", "S&P 500"}, {"VT", "Total world"}}},
		{"uppercases the symbol only", []string{"qqq=nasdaq"}, []Benchmark{{"QQQ", "nasdaq"}}},
		{"label falls back to symbol", []string{"DIA", "IWM=", "EFA =  "}, []Benchmark{{"DIA", "DIA"}, {"IWM", "IWM"}, {"EFA", "EFA"}}},
		{"splits on the first equals", []string{"X=a=b"}, []Benchmark{{"X", "a=b"}}},
		{"skips bad entries", []string{"", "  ", "=No symbol", "SPY=ok"}, []Benchmark{{"SPY", "ok"}}},
		{"nil input", nil, []Benchmark{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseBenchmarks(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
