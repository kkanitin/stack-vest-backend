package portfolio

// SuggestedReturnLookbackYears is how far back each holding's price history is read for
// its annualised return. A holding with less history uses all it has.
const SuggestedReturnLookbackYears = 10

// MinVolatilityMonths is the fewest monthly returns a volatility is worked out from. With
// less, the volatility is null: a handful of months gives a figure too noisy to simulate on.
const MinVolatilityMonths = 24

// Codes for a holding left out of a suggested return.
const (
	// SkipNoPrice: no live quote, so the holding has no current value to weight by.
	SkipNoPrice = "no_price"
	// SkipNoHistory: the provider has no price history for the symbol, or less than the
	// minimum span needed for a meaningful annual figure.
	SkipNoHistory = "no_history"
	// SkipHistoryUnavailable: the price history could not be fetched (provider failure).
	SkipHistoryUnavailable = "history_unavailable"
)

// SuggestedReturn is an expected annual return derived from what a portfolio holds: each
// open holding's annualised price return (CAGR), weighted by its current value.
//
// When nothing could be used, Available is false, AnnualReturn and Lookback are nil, and
// Note says why. This is a normal 200 response, not an error.
type SuggestedReturn struct {
	Available bool `json:"available"`
	// AnnualReturn is the value-weighted average of the holdings' CAGRs, in percent.
	AnnualReturn *float64 `json:"annualReturn"`
	// AnnualVolatility is the annualised standard deviation (percent) of the portfolio's
	// monthly log returns, the holdings weighted by today's value, over the months every
	// included holding has. Nil when there is no suggestion or fewer than
	// MinVolatilityMonths shared monthly returns.
	AnnualVolatility *float64        `json:"annualVolatility"`
	Lookback         *ReturnLookback `json:"lookback"`
	// Holdings are the holdings used, in descending weight order.
	Holdings []SuggestedReturnHolding `json:"holdings"`
	Skipped  []SkippedHolding         `json:"skipped"`
	// Note explains a missing suggestion, or that some holdings used less history.
	Note           string `json:"note,omitempty"`
	PriceBasisNote string `json:"priceBasisNote"`
}

// ReturnLookback is the period actually used across the included holdings: from the
// earliest holding start to the latest holding end. ShortHistory is true when at least one
// holding had less than RequestedYears of history.
type ReturnLookback struct {
	StartDate      string  `json:"startDate"`
	EndDate        string  `json:"endDate"`
	Years          float64 `json:"years"`
	RequestedYears int     `json:"requestedYears"`
	ShortHistory   bool    `json:"shortHistory"`
}

// SuggestedReturnHolding is one holding's contribution. WeightPct is its share (0-100) of
// the value of the included holdings; AnnualReturn is its CAGR in percent over
// [StartDate, EndDate]. AnnualVolatility is its own annualised volatility in percent, nil
// with fewer than MinVolatilityMonths monthly returns.
type SuggestedReturnHolding struct {
	Symbol           string   `json:"symbol"`
	Name             string   `json:"name"`
	ValueUsd         float64  `json:"valueUsd"`
	WeightPct        float64  `json:"weightPct"`
	AnnualReturn     float64  `json:"annualReturn"`
	AnnualVolatility *float64 `json:"annualVolatility"`
	StartDate        string   `json:"startDate"`
	EndDate          string   `json:"endDate"`
	Years            float64  `json:"years"`
	ShortHistory     bool     `json:"shortHistory"`
}

// SkippedHolding is a holding left out of the suggestion. Code is one of the Skip*
// constants; Reason is readable text.
type SkippedHolding struct {
	Symbol string `json:"symbol"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}
