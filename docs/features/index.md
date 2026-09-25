# Feature Index

This is the index for feature documentation. **Before you implement or change a feature, read this index,
then the linked feature doc.** Each feature doc covers endpoints, code map, data and dependencies, caching, and the
feature-specific rules and gotchas. Repo-wide conventions live in the [documentation index](../index.md) and
[AGENTS.md](../../AGENTS.md).

## Features

| Feature                                          | Endpoints (under `/api/v1` unless noted)                                              | Auth      | Main packages                                                   |
|--------------------------------------------------|---------------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|
| [Auth (Google OAuth)](./auth.md)                 | `GET /auth/google`, `GET /auth/google/callback`                                       | public    | `usecase/auth`, `middleware/auth.go`                            |
| [User](./user.md)                                | `GET/POST /users/me`                                                                  | protected | `domain/user`, `usecase/user`, `repository/user`                |
| [Stocks (market data)](./stock.md)               | `/stocks/search`, `/stocks/price-changes`, `/stocks/history`, `/stocks/:symbol/{quote,price-change,history,profile}` | protected | `usecase/stock`, `infrastructure/fmp`, `infrastructure/cached` |
| [Popular assets](./popular.md)                   | `GET /popular`                                                                        | public    | `handler/popular.go`                                            |
| [Watchlist](./watchlist.md)                      | `/watchlist`, `/watchlist/:symbol`, `/watchlist/:symbol/alerts`                       | protected | `domain/watchlist`, `usecase/watchlist`, `repository/watchlist` |
| [Portfolios & positions](./portfolio.md)         | `/portfolios`, `/portfolios/summary`, `/portfolios/:id[/positions,/summary,/activity]` | protected | `domain/portfolio`, `usecase/portfolio`, `repository/portfolio` |
| [AI portfolio analysis (SSE)](./portfolio-analysis.md) | `POST /portfolios/analyze`, `POST /portfolios/:id/analyze`                      | protected | `usecase/analysis`, `infrastructure/groq`                       |
| [DCA simulator](./dca.md)                        | `POST /dca/simulate`                                                                  | protected | `domain/dca`, `usecase/dca`                                     |
| [Market sentiment](./sentiment.md)               | `GET /sentiment`                                                                      | protected | `domain/sentiment`, `usecase/sentiment`                         |
| [Dividend calendar](./dividend.md)               | `GET /dividends/calendar`                                                             | protected | `usecase/dividend`, `repository/dividend` (Redis)               |

`GET /health` (outside `/api/v1`, public) is infrastructure, not a feature. It keeps its own `{"message": "ready"}`
response shape.

## Cross-feature dependencies

- **User lookup:** watchlist, portfolio and dividend turn the authenticated email into a user ID with `user.Repository.FindByEmail`.
- **Pricing:** portfolio uses the shared cached `Quoter` and `PriceChanger` from [stock.md](./stock.md), wrapped once in `main.go`.
- **Holdings:** dividend and portfolio analysis read positions from the portfolio repository and use case.
- **FMP client** (`infrastructure/fmp`) is used by stock, popular, watchlist (symbol validation), DCA, sentiment and dividend.

## Adding or changing a feature

1. Read this index and the relevant feature doc(s), including any feature listed under cross-feature dependencies.
2. Follow the layering steps in [architecture.md](../architecture.md) and the conventions in [AGENTS.md](../../AGENTS.md).
3. In the same change, create or update `docs/features/<feature>.md` using the template below, and add or update
   that feature's row in the table above.

### Feature doc template

```markdown
# <Feature name>

## Purpose
## Endpoints          — method, path, auth, body/params, response + error codes
## Code map           — domain / usecase / repository / infrastructure / handler files
## Data & dependencies — tables + migrations, Redis keys, external APIs, config keys
## Caching            — (if any) TTLs, keys, coalescing, negative caching
## Rules & gotchas    — feature-specific decisions and pitfalls
## Tests              — relevant _test.go files
```
