package sentiment

import "testing"

func TestScoreFromVIX(t *testing.T) {
	tests := []struct {
		name string
		vix  float64
		want int
	}{
		{"floor", 10, 100},
		{"ceil", 40, 0},
		{"midpoint", 25, 50},
		{"below floor clamps", 5, 100},
		{"above ceil clamps", 50, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScoreFromVIX(tc.vix); got != tc.want {
				t.Errorf("ScoreFromVIX(%v) = %d, want %d", tc.vix, got, tc.want)
			}
		})
	}
}

func TestScoreFromMomentum(t *testing.T) {
	tests := []struct {
		name          string
		changePercent float64
		want          int
	}{
		{"floor", -3, 0},
		{"ceil", 3, 100},
		{"flat", 0, 50},
		{"below floor clamps", -10, 0},
		{"above ceil clamps", 10, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScoreFromMomentum(tc.changePercent); got != tc.want {
				t.Errorf("ScoreFromMomentum(%v) = %d, want %d", tc.changePercent, got, tc.want)
			}
		})
	}
}

func TestScoreFromBreadth(t *testing.T) {
	tests := []struct {
		name            string
		gainers, losers int
		want            int
	}{
		{"all gainers", 10, 0, 100},
		{"all losers", 0, 10, 0},
		{"even split", 5, 5, 50},
		{"no movers neutral fallback", 0, 0, 50},
		{"skewed toward gainers", 7, 3, 70},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScoreFromBreadth(tc.gainers, tc.losers); got != tc.want {
				t.Errorf("ScoreFromBreadth(%d, %d) = %d, want %d", tc.gainers, tc.losers, got, tc.want)
			}
		})
	}
}

func TestCompositeScore(t *testing.T) {
	tests := []struct {
		name                                  string
		vixScore, momentumScore, breadthScore int
		want                                  int
	}{
		{"all neutral", 50, 50, 50, 50},
		{"all extremes high", 100, 100, 100, 100},
		{"all extremes low", 0, 0, 0, 0},
		{"vix-weighted dominance", 100, 0, 0, 50}, // 100*0.5 + 0 + 0
		{"mixed weights", 80, 60, 40, 66},         // 80*0.5 + 60*0.3 + 40*0.2 = 40+18+8 = 66
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompositeScore(tc.vixScore, tc.momentumScore, tc.breadthScore); got != tc.want {
				t.Errorf("CompositeScore(%d, %d, %d) = %d, want %d", tc.vixScore, tc.momentumScore, tc.breadthScore, got, tc.want)
			}
		})
	}
}

func TestStatusFromScore(t *testing.T) {
	tests := []struct {
		name  string
		score int
		want  string
	}{
		{"extreme fear low edge", 0, "Extreme Fear"},
		{"extreme fear high edge", 19, "Extreme Fear"},
		{"fear low edge", 20, "Fear"},
		{"fear high edge", 39, "Fear"},
		{"neutral low edge", 40, "Neutral"},
		{"neutral high edge", 59, "Neutral"},
		{"greed low edge", 60, "Greed"},
		{"greed high edge", 79, "Greed"},
		{"extreme greed low edge", 80, "Extreme Greed"},
		{"extreme greed high edge", 100, "Extreme Greed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusFromScore(tc.score); got != tc.want {
				t.Errorf("StatusFromScore(%d) = %q, want %q", tc.score, got, tc.want)
			}
		})
	}
}
