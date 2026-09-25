# User

## Purpose

Reads or creates the profile of the authenticated user. The Google ID token claims identify the user.

## Endpoints

| Method | Path               | Auth      | Response                                                                    |
|--------|--------------------|-----------|-----------------------------------------------------------------------------|
| GET    | `/api/v1/users/me` | protected | `200` user · `404` user not found                                           |
| POST   | `/api/v1/users/me` | protected | `201` user · `409` user already exists. Takes no body: name and picture come from token claims |

## Code map

- Domain: `internal/domain/user/user.go` (`User`, `Repository`, `ErrNotFound`, `ErrAlreadyExists`)
- Use case: `internal/usecase/user/user.go` (`FindByEmail`, `Create`)
- Repository: `internal/repository/user/postgres.go`
- Handler: `internal/delivery/http/handler/user.go`

## Data & dependencies

- Table `users` (see [auth.md](./auth.md) for migrations).

## Rules & gotchas

- The user repository is shared. Watchlist, portfolio and dividend use cases call `FindByEmail` to turn the
  authenticated email into a `user.ID`.

## Tests

`handler/user_test.go`
