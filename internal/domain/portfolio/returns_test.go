package portfolio

import (
	"math"
	"testing"
)

func twrNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCumulativeTWR_FlatPriceIsZero(t *testing.T) {
	s := []DailyPoint{{"2026-01-01", 100, 0}, {"2026-01-02", 100, 0}, {"2026-01-03", 100, 0}}
	if got := TWR(s); !twrNear(got, 0) {
		t.Fatalf("got %v want 0", got)
	}
}

func TestCumulativeTWR_CompoundsDailyReturns(t *testing.T) {
	s := []DailyPoint{{"d1", 100, 0}, {"d2", 110, 0}, {"d3", 121, 0}}
	got := CumulativeTWR(s)
	if !twrNear(got[0], 0) || !twrNear(got[1], 0.10) || !twrNear(got[2], 0.21) {
		t.Fatalf("got %v", got)
	}
}

func TestTWR_BuyingMoreAtCurrentPriceLeavesReturnUnchanged(t *testing.T) {
	// 10 shares at 100; day 2 price 110; buy 10 more at 110 (flow 1100); day 3 price 121.
	// Day 2 deposit: V2 includes new shares after the buy.
	with := []DailyPoint{{"d1", 1000, 0}, {"d2", 2200, 1100}, {"d3", 2420, 0}}
	without := []DailyPoint{{"d1", 1000, 0}, {"d2", 1100, 0}, {"d3", 1210, 0}}
	if !twrNear(TWR(with), TWR(without)) || !twrNear(TWR(with), 0.21) {
		t.Fatalf("with=%v without=%v", TWR(with), TWR(without))
	}
}

func TestTWR_OpeningEntryJumpNeutralised(t *testing.T) {
	// Nothing held on d1; a carried-over holding appears on d2 valued 5000, flagged as a
	// flow of 5000 (market value), then it rises 10%.
	s := []DailyPoint{{"d1", 0, 0}, {"d2", 5000, 5000}, {"d3", 5500, 0}}
	got := CumulativeTWR(s)
	if !twrNear(got[1], 0) || !twrNear(got[2], 0.10) {
		t.Fatalf("got %v", got)
	}
}

func TestTWR_SellIsNegativeFlow(t *testing.T) {
	// 20 shares at 100 (2000). Sell 10 at 100 (flow -1000): V=1000, return 0.
	s := []DailyPoint{{"d1", 2000, 0}, {"d2", 1000, -1000}}
	if got := TWR(s); !twrNear(got, 0) {
		t.Fatalf("got %v want 0", got)
	}
}

func TestTWR_EmptyAndSingle(t *testing.T) {
	if TWR(nil) != 0 || TWR([]DailyPoint{{"d", 5, 0}}) != 0 {
		t.Fatal("want 0")
	}
	if len(CumulativeTWR(nil)) != 0 {
		t.Fatal("want empty")
	}
}

func TestGain_ExcludesFlows(t *testing.T) {
	s := []DailyPoint{{"d1", 1000, 0}, {"d2", 2200, 1100}, {"d3", 2420, 0}}
	// (2200-1100-1000) + (2420-2200) = 100 + 220
	if got := Gain(s); !twrNear(got, 320) {
		t.Fatalf("got %v", got)
	}
}
