package sentiment

import (
	"math"
	"time"
)

// Score is the daily 0-100 "Fear & Greed" composite returned by the sentiment endpoint.
type Score struct {
	Score     int       `json:"score"`     // 0-100 composite
	Status    string    `json:"status"`    // "Extreme Fear" | "Fear" | "Neutral" | "Greed" | "Extreme Greed"
	Signals   Signals   `json:"signals"`   // raw FMP-sourced inputs behind the composite
	Timestamp time.Time `json:"timestamp"` // when the score was computed (cache-fill time)
}

// Signals exposes the raw FMP-sourced values that fed the composite, so the
// frontend can explain the gauge instead of just rendering a bare number.
type Signals struct {
	VIX                float64 `json:"vix"`                // ^VIX quote price
	IndexChangePercent float64 `json:"indexChangePercent"` // ^GSPC daily % change
	GainersCount       int     `json:"gainersCount"`       // FMP biggest-gainers list size
	LosersCount        int     `json:"losersCount"`        // FMP biggest-losers list size
}

const (
	vixFloor = 10.0 // VIX <= 10 -> sub-score 100 (calm)
	vixCeil  = 40.0 // VIX >= 40 -> sub-score 0   (panic)

	momentumFloor = -3.0 // ^GSPC daily change <= -3% -> sub-score 0
	momentumCeil  = 3.0  // ^GSPC daily change >= +3% -> sub-score 100

	weightVIX      = 0.5
	weightMomentum = 0.3
	weightBreadth  = 0.2
)

// ScoreFromVIX linearly maps VIX in [vixFloor, vixCeil] to a sub-score in
// [100, 0], clamping out-of-range values to [0, 100].
// (vix=10 -> 100, vix=40 -> 0, vix=25 -> 50)
func ScoreFromVIX(vix float64) int {
	return clampRound((vixCeil - vix) / (vixCeil - vixFloor) * 100)
}

// ScoreFromMomentum linearly maps the index's daily % change in
// [momentumFloor, momentumCeil] to a sub-score in [0, 100], clamping
// out-of-range values. (-3% -> 0, 0% -> 50, +3% -> 100)
func ScoreFromMomentum(changePercent float64) int {
	return clampRound((changePercent - momentumFloor) / (momentumCeil - momentumFloor) * 100)
}

// ScoreFromBreadth maps the gainers-vs-losers split to a sub-score in [0, 100]
// — the share of today's movers that are advancing. An empty mover list (e.g.
// a market holiday) is treated as neutral (50) rather than divide-by-zero.
func ScoreFromBreadth(gainers, losers int) int {
	total := gainers + losers
	if total == 0 {
		return 50
	}
	return clampRound(float64(gainers) / float64(total) * 100)
}

// CompositeScore blends the three sub-scores using fixed weights — VIX stays
// the dominant signal, momentum and breadth add FMP-sourced context on top.
func CompositeScore(vixScore, momentumScore, breadthScore int) int {
	raw := float64(vixScore)*weightVIX +
		float64(momentumScore)*weightMomentum +
		float64(breadthScore)*weightBreadth
	return clampRound(raw)
}

// StatusFromScore buckets a 0-100 score into the five sentiment labels using
// equal-width 20-point bands.
func StatusFromScore(score int) string {
	switch {
	case score < 20:
		return "Extreme Fear"
	case score < 40:
		return "Fear"
	case score < 60:
		return "Neutral"
	case score < 80:
		return "Greed"
	default:
		return "Extreme Greed"
	}
}

func clampRound(raw float64) int {
	switch {
	case raw < 0:
		return 0
	case raw > 100:
		return 100
	default:
		return int(math.Round(raw))
	}
}
