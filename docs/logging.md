# Logging

Read this before adding or changing log calls.

**Package:** `pkg/logger` wraps `go.uber.org/zap`. `main.go` calls `zap.ReplaceGlobals` once at startup so all code can use the package-level `zap.L().Info/Warn/Error/Debug` functions without carrying a logger reference. `middleware.Logger` is the one exception — it receives its `*zap.Logger` via constructor injection instead.

**Configuration** (env vars or `config.yaml`):

| Env var | `config.yaml` key | Default | Values |
|---|---|---|---|
| `LOG_LEVEL` | `log.level` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `log.format` | `json` | `json`, `text` |

Use `text` locally for human-readable console output; keep `json` in staging/production for log aggregators.

**Where to log:**

- **Errors only at the handler layer.** Use `zap.L().Error("short description", logger.RequestID(ctx), zap.String("key", val), zap.Error(err))`. Do not log the same error at multiple layers — wrap it with `fmt.Errorf` up the call chain, then log once at the top.
- **Do not log in use-case or repository layers** unless the error is swallowed (not returned). If an error is returned to the caller, the caller logs it.
- **HTTP requests** are logged automatically by `middleware.Logger` (method, path, status, latency, client IP, request ID). Do not duplicate request/response logging in handlers.
- **Startup events** (server starting, DB connected) go in `main.go` with `zap.L().Info`.

**Request correlation:** `middleware.Logger` generates a request ID per inbound request (`pkg/requestid`), stores it on the request context, and returns it via the `X-Request-ID` response header. Any log call with a `context.Context` in scope should include `logger.RequestID(ctx)` as a field so it can be correlated with that request's summary line. It renders as a no-op field when ctx carries no ID. This covers the few call sites that have no context parameter at all, such as background cache fills and low-level client traces.

**Attribute conventions:**

```go
// Typed fields, not loose key-value pairs
zap.L().Error("payment failed", logger.RequestID(ctx), zap.String("orderID", id), zap.Error(err))
zap.L().Info("user upserted", logger.RequestID(ctx), zap.String("userID", user.ID))
```

- Key names: `lowerCamelCase` (`userID`, `orderID`, `error`).
- Always include `zap.Error(err)` for error logs.
- Never log secrets, tokens, passwords, or PII.
