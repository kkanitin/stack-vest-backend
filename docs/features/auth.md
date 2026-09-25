# Auth (Google OAuth)

## Purpose

Signs users in with Google. The OAuth callback upserts the user and returns an app-signed JWT. Protected routes
authenticate with a **Google ID token** passed as a Bearer token.

## Endpoints

| Method | Path                           | Auth   | Notes                                                                                  |
|--------|--------------------------------|--------|----------------------------------------------------------------------------------------|
| GET    | `/api/v1/auth/google`          | public | Sets the `oauth_state` cookie (10 min, HttpOnly, SameSite=Lax), then does a 307 redirect to Google |
| GET    | `/api/v1/auth/google/callback` | public | Query `code`, `state`. Returns `{ token, user }` in the single-object envelope          |

Callback errors: `400` when `code` is missing or `state` doesn't match the cookie, `500` when the code exchange,
user-info call or JWT signing fails.

## Code map

- Handler: `internal/delivery/http/handler/auth.go`
- Use case: `internal/usecase/auth/google.go` (`GoogleUseCase`: `GetAuthURL`, `HandleCallback`)
- Middleware: `internal/delivery/http/middleware/auth.go` (`Auth(googleClientID)`)
- Repository: `internal/repository/user/postgres.go` (`Upsert`, `ON CONFLICT (google_id)`)

## Data & dependencies

- Table `users`. Migrations `000001_create_users`, `000010_add_users_google_id_unique`.
- Config: `auth.google.client_id`, `auth.google.client_secret`, `auth.google.redirect_url`, `auth.jwt.secret`.
  The server refuses to start when `auth.jwt.secret` is empty.

## Rules & gotchas

- **CSRF:** the `state` nonce is single-use. The callback clears the cookie before it compares the state.
- **JWT:** HS256 signed with `auth.jwt.secret`. Claims are `sub`, `email`, `name`, `picture`, and `exp` (7 days).
- **Protected routes** (`middleware.Auth`) validate a **Google ID token** with `idtoken.Validate` against the Google
  client ID and require `email_verified`. They don't validate the app JWT. On success the middleware sets
  `middleware.UserIDKey`, `EmailKey`, `NameKey` and `PictureKey` on the Gin context. Handlers identify the caller with
  `c.GetString(middleware.EmailKey)`.
- `isSecureRequest` only checks `c.Request.TLS`. Behind a TLS-terminating proxy the state cookie is sent without the
  `Secure` flag. This is accepted for a short-lived nonce.
- `decodeUserInfo` checks the HTTP status before it decodes. Without that check, a Google error body would decode
  into an empty user.

## Tests

`handler/auth_test.go`, `middleware/auth_test.go`, `usecase/auth/google_test.go`
