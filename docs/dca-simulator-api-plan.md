# DCA Simulator API Plan

## Overview

A Dollar-Cost Averaging (DCA) simulation feature that shows what returns a user would have achieved by investing a fixed amount at regular intervals over a historical period. This is a **stateless, read-only computation** — no database tables are needed.

**FMP-first approach:** Prefer using FMP API endpoints directly. Only build custom logic on top of FMP responses when no native endpoint covers the need. Building from scratch is lowest priority.

---

## Endpoint

```
POST /api/v1/dca/simulate
```

Protected — JWT required.

### Request Body

```json
{
  "symbol":    "AAPL",
  "startDate": "2020-01-01",
  "endDate":   "2024-12-31",
  "amount":    100.00,
  "frequency": "monthly"
}
```

| Field | Type | Rules |
|---|---|---|
| `symbol` | string | required, non-empty; normalized to **uppercase** before calling FMP |
| `startDate` | string | `YYYY-MM-DD`, required, must be before `endDate` |
| `endDate` | string | `YYYY-MM-DD`, required, must be <= today |
| `amount` | float64 | required, > 0 |
| `frequency` | string | must be exactly one of `daily`, `weekly`, `biweekly`, `monthly`; validated against domain constants — any other value returns 400 |

### Success Response

```json
{
  "result": {
    "symbol":                "AAPL",
    "startDate":             "2020-01-01",
    "endDate":               "2024-12-31",
    "frequency":             "monthly",
    "amountPerPeriod":       100.00,
    "totalInvested":         6000.00,
    "finalPortfolioValue":   11234.56,
    "totalReturn":           5234.56,
    "totalReturnPct":        87.24,
    "annualizedReturnPct":   13.51,
    "annualizedReturnNote":  "CAGR-based estimate (total-capital basis, not IRR)",
    "periodsCount":          60,
    "totalUnits":            42.3178,
    "dataPoints": [
      {
        "date":            "2020-01-02",
        "price":           75.09,
        "unitsPurchased":  1.3318,
        "totalUnits":      1.3318,
        "totalInvested":   100.00,
        "portfolioValue":  100.00,
        "returnPct":       0.00
      }
    ]
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

### Error Responses

| Scenario | HTTP Status | `errorMessage` |
|---|---|---|
| Missing / invalid field | 400 | field-specific message |
| Invalid `frequency` value | 400 | `"frequency must be one of: daily, weekly, biweekly, monthly"` |
| `startDate >= endDate` | 400 | `"startDate must be before endDate"` |
| `endDate` in the future | 400 | `"endDate cannot be in the future"` |
| Date range too short (< 2 periods) | 400 | `"date range too short for the selected frequency"` |
| Symbol not found on FMP | 404 | `"symbol not found: XYZZ"` |
| FMP request failure | 500 | `"failed to fetch historical prices"` |

---

## Architecture

The feature follows the existing Clean Architecture pattern: **domain → usecase → infrastructure → handler**.

### Files to Create

| File | Purpose |
|---|---|
| `internal/domain/dca/dca.go` | Domain types, `PriceFetcher` interface, sentinel errors |
| `internal/usecase/dca/simulator.go` | Simulation business logic |
| `internal/usecase/dca/simulator_test.go` | Unit tests with mock `PriceFetcher` |
| `internal/delivery/http/handler/dca.go` | Gin handler + request binding |

### Files to Modify

| File | Change |
|---|---|
| `internal/infrastructure/fmp/client.go` | Add `GetHistoricalPrices()` using `adjClose` |
| `internal/delivery/http/router/router.go` | Add `dcaHandler` param, call `RegisterRoutes` |
| `main.go` | Wire `dcaUC` and `dcaHandler`, pass to `router.New()` |

---

## Domain Types (`internal/domain/dca/dca.go`)

```go
type Frequency string

const (
    FrequencyDaily    Frequency = "daily"
    FrequencyWeekly   Frequency = "weekly"
    FrequencyBiweekly Frequency = "biweekly"
    FrequencyMonthly  Frequency = "monthly"
)

func (f Frequency) IsValid() bool {
    switch f {
    case FrequencyDaily, FrequencyWeekly, FrequencyBiweekly, FrequencyMonthly:
        return true
    }
    return false
}

type SimulationInput struct {
    Symbol    string    // always uppercase
    StartDate time.Time
    EndDate   time.Time
    Amount    float64
    Frequency Frequency
}

type HistoricalPrice struct {
    Date     time.Time
    AdjClose float64   // adjusted close — accounts for splits and dividends
}

type DataPoint struct {
    Date           string  `json:"date"`
    Price          float64 `json:"price"`           // adjClose used for calculation
    UnitsPurchased float64 `json:"unitsPurchased"`
    TotalUnits     float64 `json:"totalUnits"`
    TotalInvested  float64 `json:"totalInvested"`
    PortfolioValue float64 `json:"portfolioValue"`
    ReturnPct      float64 `json:"returnPct"`
}

type SimulationResult struct {
    Symbol              string      `json:"symbol"`
    StartDate           string      `json:"startDate"`
    EndDate             string      `json:"endDate"`
    Frequency           Frequency   `json:"frequency"`
    AmountPerPeriod     float64     `json:"amountPerPeriod"`
    TotalInvested       float64     `json:"totalInvested"`
    FinalPortfolioValue float64     `json:"finalPortfolioValue"`
    TotalReturn         float64     `json:"totalReturn"`
    TotalReturnPct      float64     `json:"totalReturnPct"`
    AnnualizedReturnPct float64     `json:"annualizedReturnPct"`
    AnnualizedReturnNote string     `json:"annualizedReturnNote"`
    PeriodsCount        int         `json:"periodsCount"`
    TotalUnits          float64     `json:"totalUnits"`
    DataPoints          []DataPoint `json:"dataPoints"`
}

// PriceFetcher is implemented by the FMP client.
type PriceFetcher interface {
    GetHistoricalPrices(symbol string, from, to time.Time) ([]HistoricalPrice, error)
}

var (
    ErrSymbolNotFound    = errors.New("symbol not found")
    ErrDateRangeTooShort = errors.New("date range too short for the selected frequency")
)
```

---

## FMP Historical Price Endpoint

FMP endpoint to add to the client:

```
GET https://financialmodelingprep.com/stable/historical-price-eod/full/{symbol}
    ?from=YYYY-MM-DD&to=YYYY-MM-DD&apikey=KEY
```

Response shape:
```json
{
  "symbol": "AAPL",
  "historical": [
    { "date": "2024-12-31", "close": 258.30, "adjClose": 257.85 },
    ...
  ]
}
```

Implementation notes:
- Map `adjClose` field (not `close`) into `HistoricalPrice.AdjClose` — this accounts for stock splits and dividends, giving accurate long-term return figures.
- FMP returns dates in **descending** order — reverse the slice to ascending before returning.
- Empty `historical` array → return `dca.ErrSymbolNotFound`.
- Add compile-time check: `var _ dca.PriceFetcher = (*Client)(nil)`.

---

## Simulation Calculation Logic (`internal/usecase/dca/simulator.go`)

### Step 1 — Fetch & index prices

Call `PriceFetcher.GetHistoricalPrices`. Build `map[string]float64` keyed by `"YYYY-MM-DD"` → `AdjClose` for O(1) lookup. Collect the sorted trading day slice in ascending order.

### Step 2 — Select investment dates (Next Trading Day strategy)

For each frequency, determine **target dates** from the calendar and resolve each to the **first available trading day on or after** that target. This handles weekends and market holidays transparently.

| Frequency | Target date per period |
|---|---|
| `daily` | every trading day in the range (no calendar target needed) |
| `weekly` | the Monday of each calendar week in the range |
| `biweekly` | the Monday of every other calendar week, anchored to the week containing `startDate` |
| `monthly` | the 1st of each calendar month in the range |

**"Next Trading Day" resolution:**
```
func nextTradingDay(target time.Time, priceMap map[string]float64) (date string, price float64, found bool) {
    for i := 0; i < 7; i++ {   // look ahead up to 7 calendar days
        key := target.AddDate(0, 0, i).Format("2006-01-02")
        if p, ok := priceMap[key]; ok {
            return key, p, true
        }
    }
    return "", 0, false   // no trading day found (e.g. extended holiday)
}
```

A 7-day lookahead covers any market holiday stretch. If no trading day is found within 7 days, that period is skipped silently (avoids erroring on unusually long holiday windows).

### Step 3 — Guard

If fewer than 2 investment dates are resolved → return `ErrDateRangeTooShort`.

### Step 4 — DCA loop

```
totalUnits    := 0.0
totalInvested := 0.0

for each investmentDate in resolvedDates:
    price           = priceMap[investmentDate]
    unitsPurchased  = amount / price
    totalUnits     += unitsPurchased
    totalInvested  += amount
    portfolioValue  = totalUnits * price
    returnPct       = ((portfolioValue - totalInvested) / totalInvested) * 100

    append DataPoint{date, price, unitsPurchased, totalUnits, totalInvested, portfolioValue, returnPct}
```

### Step 5 — Final stats

Final portfolio value uses the last price in the fetched slice (most recent trading day within the requested range).

```
totalReturn      = finalPortfolioValue - totalInvested
totalReturnPct   = (totalReturn / totalInvested) * 100

years            = endDate.Sub(startDate).Hours() / (365.25 * 24)
annualizedReturn = (math.Pow(finalPortfolioValue/totalInvested, 1/years) - 1) * 100
```

---

## Annualized Return: CAGR vs IRR

The plan uses **CAGR** (Compound Annual Growth Rate) for `annualizedReturnPct`:

```
CAGR = (finalValue / totalInvested)^(1/years) - 1
```

**Limitation:** CAGR treats `totalInvested` as a single lump-sum from day one. For DCA, where capital is deployed incrementally, the correct metric is **IRR** (Internal Rate of Return / money-weighted return), which accounts for the timing of each cash flow.

**Decision for now:** Ship with CAGR and include `annualizedReturnNote: "CAGR-based estimate (total-capital basis, not IRR)"` in the response so the frontend can surface a disclosure to users. IRR implementation (Newton-Raphson or bisection on the NPV equation) can be added as a follow-up without a breaking API change — just populate the field with the IRR value and update the note.

---

## Performance Consideration

For long date ranges at high frequency, `dataPoints` can become very large:

| Scenario | Approximate `dataPoints` count |
|---|---|
| 5 years, monthly | ~60 |
| 10 years, weekly | ~520 |
| 20 years, daily | ~5,000+ |

**Mitigation strategy (phase 1):** Add a hard limit on the simulated time span per frequency:

| Frequency | Max date range |
|---|---|
| `daily` | 5 years |
| `weekly` | 15 years |
| `biweekly` | 20 years |
| `monthly` | 30 years |

Return HTTP 400 with `"date range exceeds the maximum allowed for the selected frequency"` if exceeded. This caps `dataPoints` at ~1,300 entries in the worst case and avoids unbounded memory allocation.

**Future option:** If unlimited ranges are needed later, add a `summaryOnly: true` flag to the request that omits `dataPoints` from the response and returns only the summary statistics.

---

## Validation Refinements

### Handler (`internal/delivery/http/handler/dca.go`)

```go
type simulateDCARequest struct {
    Symbol    string  `json:"symbol"    binding:"required"`
    StartDate string  `json:"startDate" binding:"required"`
    EndDate   string  `json:"endDate"   binding:"required"`
    Amount    float64 `json:"amount"    binding:"required,gt=0"`
    Frequency string  `json:"frequency" binding:"required"`
}
```

After `ShouldBindJSON`:
1. `symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))` — normalize before any downstream call.
2. Parse `startDate` and `endDate` with `time.Parse("2006-01-02", ...)` — return 400 on parse failure.
3. Guard `endDate <= time.Now().UTC().Truncate(24 * time.Hour)`.
4. Guard `startDate.Before(endDate)`.
5. `freq := domain.Frequency(req.Frequency); if !freq.IsValid() { response.Err(...) }` — explicit enum check before reaching the usecase.
6. Check date range against per-frequency max (see Performance section).

---

## Unit Tests (`internal/usecase/dca/simulator_test.go`)

Use a mock `PriceFetcher` that returns deterministic price data (e.g., flat $100/day, or a known rising series). Verify:

| Test case | What to assert |
|---|---|
| Monthly over 3 months (flat $100 price) | `periodsCount = 3`, `totalInvested = amount * 3`, `totalUnits = 3`, `returnPct = 0` |
| Monthly rising price | `totalReturnPct > 0`, `finalPortfolioValue > totalInvested` |
| Investment date falls on weekend | Resolves to next Monday (next trading day in mock data) |
| Biweekly anchoring | Only every other week selected, first week always included |
| Single period in range | Returns `ErrDateRangeTooShort` |
| Empty price response | Returns `ErrSymbolNotFound` |
| CAGR formula | Known inputs produce expected `annualizedReturnPct` within float tolerance |

---

## Wiring (`main.go`)

```go
// avClient already exists
dcaUC      := dcauc.NewSimulatorUseCase(avClient)
dcaHandler := handler.NewDCAHandler(dcaUC)

r := router.New(stockHandler, authHandler, userHandler, watchlistHandler, dcaHandler, ...)
```

No database migrations required.

---

## Verification

1. Start server: `go run ./...`
2. Authenticate and obtain a JWT via Google OAuth.
3. `POST /api/v1/dca/simulate` with a valid body (e.g., AAPL, 2020-01-01 → 2024-12-31, $100/month).
4. Verify `periodsCount ≈ 60` and `totalInvested = periodsCount × amountPerPeriod`.
5. Verify `price` fields in `dataPoints` reflect split-adjusted prices (compare against a financial reference).
6. Test date resolution: choose a `startDate` that is a known weekend — first `dataPoint.date` should be the following Monday.
7. Test error cases: unknown symbol → 404, future `endDate` → 400, invalid `frequency` → 400, range exceeding limit → 400.
8. Run unit tests: `go test ./internal/usecase/dca/...`
