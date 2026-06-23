# Plan: Smart asset search — match by ticker *and* company name/brand

> Status: implemented 2026-06-23 (backend search-symbol + search-name merge, plus stock/ETF-only
> filter against FMP's symbol universe). Authored 2026-06-23.
> Still needs live-key validation: (a) the `google` brand-alias risk (§5 fallback); (b) the
> stock/ETF filter — confirm `/stock-list` + `/etfs-list` paths return data and that search vs. list
> symbol formats match so the filter doesn't drop valid stocks (see Verification 2c).

## Context

Today, finding Google's stock requires typing the ticker (`GOO` / `GOOG`). Typing `google` or
`alphabet` returns nothing. The goal is for all four — `GOO`, `GOOG`, `google`, `alphabet` — to find
the result.

**Root cause (backend, not frontend).** The Go backend is a thin proxy to Financial Modeling Prep
(FMP). Its search use-case calls **only** FMP's `/stable/search-symbol` endpoint
(`backend/internal/infrastructure/fmp/client.go:83` → `SearchUseCase.Execute` at
`backend/internal/usecase/stock/search.go:17`). `search-symbol` is ticker-oriented, so name/brand
queries miss. FMP exposes a **separate** `/stable/search-name` endpoint that resolves a typed
company/brand name to its tradable ticker(s) (e.g. "Apple" → AAPL; FMP documents the
"Google → Alphabet/GOOGL" alias case for exactly this endpoint).

**The fix is in the backend.** The data contract (`StockSearchResult`: symbol, name, type, region,
currency) stays identical, so the change is isolated to the search use-case and FMP client.

### Coverage & one risk to validate
| Query | Covered by |
|-------|-----------|
| `GOO`, `GOOG` | `/search-symbol` (already works) |
| `alphabet` | `/search-name` on legal name "Alphabet Inc." — covered by adding name search |
| `google` (brand alias) | `/search-name` **only if FMP indexes the brand alias** — see risk below |

> **Risk:** "google" is a *brand alias*, not the legal name. FMP's docs/marketing say `search-name`
> handles this ("a user might input Google… data is under Alphabet's symbol"), but the live FMP key
> is deny-listed in the dev environment so this wasn't proven against the real endpoint. **During
> implementation, validate `google` against the live key.** If FMP's `search-name` does **not**
> return Alphabet for "google", add the documented fallback (below). `alphabet`, `GOO`, `GOOG` do not
> depend on this.

## Design

**Query both FMP endpoints concurrently in the use-case, merge, dedup by symbol.** This is robust
regardless of exactly how FMP splits symbol-vs-name matching: symbol queries hit `search-symbol`,
name/brand queries hit `search-name`, and the union covers all four inputs.

- Error policy: **degrade gracefully** — if one endpoint errors but the other succeeds, return the
  successful results. Only return an error when **both** fail (preserves today's behavior on total
  FMP outage).
- Ordering/dedup: emit `search-symbol` matches first (the user typed a ticker-like string), then
  append `search-name` matches whose symbol hasn't already been seen. Dedup keyed on `Symbol`.
- The handler (`backend/internal/delivery/http/handler/stock.go:83`) already paginates the merged
  slice — no handler change needed.
- **Stock/ETF-only filter.** After merge+dedup, drop any result that isn't a real stock or ETF
  (indices like `^GSPC`, forex, crypto, etc.). Membership is checked against FMP's full symbol
  universe (`/stock-list` + `/etfs-list`), cached in-process with a 24h TTL (reuses `pkg/cache.TTL`,
  same lazy-fill pattern as the sentiment use-case). Stock and ETF symbols are kept as two separate
  sets (preserves the stock-vs-ETF distinction for future use); a result passes if its symbol is in
  either set. Matching is case-insensitive (symbols upper-cased on both sides).
- **Fail-safe filter.** The filter must never wipe out results when the universe can't be trusted.
  If either list errors **or returns empty** (e.g. deny-listed key / wrong path returning an empty
  `200`), `loadUniverse` returns nil and results pass through **unfiltered** (a warning is logged).
  When the filter does drop results, the dropped count is logged at debug for live observability.

## Backend changes (primary)

### 1. `backend/internal/infrastructure/fmp/client.go`
- Factor the existing `SearchSymbol` body into a private helper `doSearch(path, keywords)` that hits
  `c.baseURL + path` and maps `fmpSearchResult` → `[]stock.Match` (reuse the existing
  `fmpSearchResult` struct and mapping at lines 75–112; `search-name` returns the same fields).
- `SearchSymbol(keywords)` → `c.doSearch("/search-symbol", keywords)`.
- Add `SearchName(keywords)` → `c.doSearch("/search-name", keywords)`.
- Keep the `var _ stock.Searcher = (*Client)(nil)` compile-time assertion — confirm it
  still compiles after the interface grows.
- Add bulk symbol-list methods backing the stock/ETF filter: a private `listSymbols(path)` helper
  (decodes only the `symbol` field), plus `ListStockSymbols()` → `/stock-list` and `ListETFSymbols()`
  → `/etfs-list`. Add `var _ stock.SymbolLister = (*Client)(nil)`.

### 2. `backend/internal/domain/stock/stock.go`
- Extend the `Searcher` interface with `SearchName(keywords string) ([]Match, error)`.
- Add a `SymbolLister` interface (`ListStockSymbols`/`ListETFSymbols`, both `() ([]string, error)`)
  for the universe filter.
- Impact check: the only other `Searcher` consumer is the watchlist use-case
  (`watchlist.go:30`, calls `SearchSymbol` only) — unaffected. FMP `Client` is the only production
  implementer. The handler test mocks the **use-case**, not the interface, so it's unaffected. No
  existing test mocks `Searcher` directly.

### 3. `backend/internal/usecase/stock/search.go`
- `NewSearchUseCase(searcher, lister, universeTTL)` — now also takes a `SymbolLister` and a TTL, and
  holds a `cache.TTL[*symbolUniverse]`.
- In `Execute`, after the empty-keywords guard, fire `SearchSymbol` and `SearchName` concurrently
  (two goroutines + `sync.WaitGroup`), merge+dedup, then apply the stock/ETF filter. Return error
  only if both search calls fail.
- `loadUniverse` lazily fetches both lists concurrently on cache miss, builds the two upper-cased
  sets, and caches them — returning nil (→ unfiltered) on any error or empty list (fail-safe).

### 4. `backend/main.go`
- Update the wiring: `stockuc.NewSearchUseCase(avClient, avClient, 24*time.Hour)` (`avClient`
  satisfies both `Searcher` and `SymbolLister`).

### 5. (Fallback, only if `google` validation fails) brand-alias map
- If live testing shows FMP `search-name` doesn't resolve `google` → Alphabet, add a tiny
  normalization step in `Execute`: a `map[string]string` of common brand aliases
  (`"google" → "alphabet"`, extensible) applied to the keyword before the name search (or as an
  extra symbol lookup). Keep it minimal and documented; do not build a general synonym engine.

## Tests

- **`backend/internal/infrastructure/fmp/client_test.go`** — `TestSearchNameParsing` (mirrors
  `TestSearchSymbolParsing`: stub httptest returning FMP `search-name` JSON, assert field mapping +
  `/search-name` path), plus `TestListStockSymbols`/`TestListETFSymbols` (assert path + symbol parse,
  blank symbols skipped).
- **`backend/internal/usecase/stock/search_test.go`** — table tests against a fake `Searcher` +
  `SymbolLister` (one mock implements all four methods):
  - symbol-only hit / name-only hit (the `google`/`alphabet` case);
  - overlap → deduped by symbol, symbol-match ordered first;
  - ETF result kept; non-stock/ETF (index `^GSPC`) filtered out; case-insensitive membership;
  - one search endpoint errors, other succeeds → successful results returned, no error;
  - both search endpoints error → error;
  - universe fetch errors **and** empty universe → results returned **unfiltered** (fail-safe).
- Existing handler test (`stock_test.go`, mocks the use-case) stays green unchanged.

## Verification
1. Backend: `go build ./...` and `go test ./...` from `C:\project\StackVest\backend` — all green;
   the `var _ stock.Searcher = (*Client)(nil)` assertion compiles.
2. **Live FMP check (decisive for the `google` risk):** run the backend with the real key and
   `GET /api/v1/stocks/search?keywords=google`, then `=GOOG`, `=GOO`, `=alphabet`. All four must
   return Alphabet (GOOGL/GOOG). If `google` is empty, apply the §4 fallback and re-test.
2b. Confirm only **two** search calls per request (one `search-symbol`, one `search-name`) — plus the
    one-time `/stock-list` + `/etfs-list` fetch per 24h TTL window — and that a deliberately broken
    `search-name` still yields symbol results (graceful degrade).
2c. **Stock/ETF filter (live, decisive):** confirm the actual complained-about query now excludes the
    non-stock/ETF result, while real tickers survive. Watch the logs: a `stock/ETF universe
    unavailable` warning means the filter silently no-op'd (check `/stock-list` + `/etfs-list` paths
    and the key), and the `filtered non-stock/etf results` debug line shows the drop count — an
    implausibly high drop rate means search-vs-list **symbol formats differ** (e.g. exchange
    suffixes) and membership is mis-matching valid stocks.