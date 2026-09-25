# AI portfolio analysis (SSE)

## Purpose

Streams an LLM-generated portfolio review (Groq, OpenAI-compatible API) to the client as Server-Sent Events.
The model is prompted to return a single JSON object with a `summary` and five scored `dimensions`.

## Endpoints

Both endpoints are protected.

| Method | Path                              | Body                                                                                                   | Pre-stream errors         |
|--------|-----------------------------------|--------------------------------------------------------------------------------------------------------|---------------------------|
| POST   | `/api/v1/portfolios/analyze`      | `{ portfolio: { name*, description, holdings*: [{ ticker*, actual ≥0, target ≥0 }] }, dimensions*: [] }` (stateless) | 400, 429, 502             |
| POST   | `/api/v1/portfolios/:id/analyze`  | `{ dimensions*: [] }`. Holdings come from the stored portfolio                                          | 400, 404, 429, 502, 500   |

For `/:id/analyze`, holdings are weighted by current market value. Unpriced symbols are dropped and the remaining
weights are renormalized. Target = actual, so the model sees zero drift. It returns `400` for an empty portfolio and
`502` when no holding could be priced.

## Response contract (exception to the standard envelope)

- **Pre-stream errors** use `response.Err`: `400` bad body, `429` Groq rate limited (every model returned 429),
  `502` upstream failure. The Groq status is mapped **before the first byte** is written.
- **Success** returns `200` with `Content-Type: text/event-stream` and `Cache-Control: no-cache`. Each upstream line
  is forwarded and flushed. The upstream `data: [DONE]` is dropped, and exactly one `data: [DONE]` is sent at the end.
  After streaming starts, the envelope no longer applies.

## Code map

- Domain: `internal/domain/analysis/analysis.go` (`Streamer`, `ErrRateLimited`, `ErrUpstream`)
- Infrastructure: `internal/infrastructure/groq/client.go`
- Use case: `internal/usecase/analysis/analyze.go` (system prompt + `buildUserPrompt`)
- Portfolio data: `usecase/portfolio.BuildAnalysisData`
- Handler: `internal/delivery/http/handler/portfolio.go` (`analyze`, `analyzePortfolio`, shared `streamAnalysis`)

## Data & dependencies

- Config: `third_party_api.groq.api_key` (env `THIRD_PARTY_API_GROQ_API_KEY`).
- Groq model fallback chain, tried in order until one returns 2xx: `openai/gpt-oss-120b` → `openai/gpt-oss-20b` →
  `qwen/qwen3.6-27b`. `max_completion_tokens` is 1600 (reasoning tokens count against it). `reasoning_format` is
  `hidden`, so reasoning never reaches the client.

## Rules & gotchas

- **Keep these routes streamable:**
  - The `Gzip` middleware skips any path ending in `/analyze`. A new SSE route must keep that suffix or update the
    exclusion.
  - `http.Server` has no `WriteTimeout`, because a global deadline would cut off the stream.
  - Don't set `Connection: keep-alive`: it is illegal over HTTP/2.
- **Prompt injection:** the portfolio description goes into a `<user_context>` block, and the system prompt tells the
  model to treat it as data.
- **Validation:** `analyzeRequest` is the reference example for nested validation. It uses `binding:"required,min=1,dive"`
  on the holdings slice of structs, and `dive,required` on `[]string` dimensions.
- Live progressive delivery has only been checked with unit tests (`httptest` buffers). To confirm it end to end, use
  `curl -N` with a real Groq key and Google token.

## Tests

`handler/portfolio_analyze_test.go`, `usecase/analysis/analyze_test.go`, `infrastructure/groq/client_test.go`
