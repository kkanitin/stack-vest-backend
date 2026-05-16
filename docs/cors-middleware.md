# Plan: Add CORS Middleware for Frontend Integration

## Context
The browser sends an HTTP `OPTIONS` preflight request before any cross-origin API call (CORS). The backend currently has no CORS middleware, so `OPTIONS /api/v1/users/me` (and all other routes) return **404**, blocking the frontend from making any request. This must be fixed on the backend.

## Root Cause
`internal/delivery/http/router/router.go` — no CORS middleware is registered. The Gin engine only has `gin.Recovery()` and a custom `Logger`. No route handles `OPTIONS` methods, so Gin returns 404.

## Fix: Add `github.com/gin-contrib/cors` Middleware

### Step 1 — Add dependency
```
go get github.com/gin-contrib/cors
```

### Step 2 — Update router (`internal/delivery/http/router/router.go`)
Register `cors.New(...)` before all other middleware:

```go
import "github.com/gin-contrib/cors"

r := gin.New()
r.Use(gin.Recovery())
r.Use(cors.New(cors.Config{
    AllowOrigins:     []string{"http://localhost:3000"}, // frontend dev origin
    AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
    AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           12 * time.Hour,
}))
r.Use(middleware.Logger(log))
```

> `AllowOrigins` should come from config/env so it can differ between dev and prod. If that config field doesn't exist yet, add it; otherwise hard-code for dev as a starting point.

## Critical Files
- `internal/delivery/http/router/router.go` — add CORS middleware
- `go.mod` / `go.sum` — updated by `go get`
- (Optional) `config/` — add `CORSAllowOrigins []string` field if config already has an env-loading pattern

## Verification
1. Run `go run ./...` — server starts without error
2. From browser (or curl): `curl -X OPTIONS http://localhost:<port>/api/v1/users/me -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" -v`
3. Response should be `204` with `Access-Control-Allow-Origin: http://localhost:3000` header
4. Frontend request should now succeed (no CORS error in browser console)