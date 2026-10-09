package portfolio

// DailyPoint is one day of a reconstructed portfolio series: the closing market Value
// of all holdings and the net external Flow that day (buys qty*price+fee minus sells
// qty*price-fee; an opening entry counts as a flow at that day's market value).
type DailyPoint struct {
	Date  string
	Value float64
	Flow  float64
}

// CumulativeTWR returns the cumulative time-weighted return (a fraction, 0.1 = 10%) at
// each point, relative to the first point (which is 0). The daily return is
// (V_d - F_d) / V_{d-1} - 1; days whose previous value is 0 are skipped (the return
// carries over unchanged). Pure function.
func CumulativeTWR(series []DailyPoint) []float64 {
	out := make([]float64, len(series))
	growth := 1.0
	for i := 1; i < len(series); i++ {
		if prev := series[i-1].Value; prev > 0 {
			growth *= (series[i].Value - series[i].Flow) / prev
		}
		out[i] = growth - 1
	}
	return out
}

// TWR is the cumulative time-weighted return over the whole series (0 when it has
// fewer than two points).
func TWR(series []DailyPoint) float64 {
	c := CumulativeTWR(series)
	if len(c) == 0 {
		return 0
	}
	return c[len(c)-1]
}

// Gain is the money gained over the series excluding external flows: the sum over days
// after the first of V_d - F_d - V_{d-1}.
func Gain(series []DailyPoint) float64 {
	var g float64
	for i := 1; i < len(series); i++ {
		g += series[i].Value - series[i].Flow - series[i-1].Value
	}
	return g
}
