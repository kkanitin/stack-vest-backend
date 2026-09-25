# HTTP conventions

Read this before writing or changing a handler, request DTO or response shape.

**Route registration convention:** each handler owns its routes via a `RegisterRoutes(*gin.RouterGroup)` method.
`router.go` stays thin — it only calls each handler's `RegisterRoutes`. Never inline route registration inside
`router.go`.

**API response convention:** all JSON response fields use `lowerCamelCase` (e.g. `marketOpen`, `matchScore`). Apply this to all `json:"..."` struct tags.

**Standard response envelope:** all handlers (except `/health`) must use the helpers in `internal/delivery/http/response/` instead of raw `gin.H`. Two shapes are supported:

- **Single object** — `response.OK(c, result)` / `response.Created(c, result)`:
  ```json
  { "result": {}, "code": 200, "message": "Success", "errorMessage": null }
  ```
- **List** — `response.OKList(c, results, response.Meta{...})`:
  ```json
  { "results": [], "code": 200, "message": "Success", "errorMessage": null, "meta": { "total": 0, "page": 1, "size": 0, "currentPageCount": 0 } }
  ```
- **Error** — `response.Err(c, statusCode, "human readable message")`:
  ```json
  { "result": null, "code": 4xx/5xx, "message": "Error", "errorMessage": "..." }
  ```

`errorMessage` is `*string` — it defaults to `null` on success and is only set to a string value on error.

The `/health` endpoint is an explicit exception and keeps its own `{"message": "ready"}` shape.

**Streaming (SSE) endpoints** are the only other allowed exception:

- Pre-stream errors still use `response.Err`. Once streaming begins, the envelope no longer applies.
- Every streaming endpoint must be documented as an exception in its feature doc.
- It must stay excluded from the `Gzip` middleware and from any server write timeout.

**Field-level validation:** `go-playground/validator` (via Gin's `binding` engine — it is the default and only field-level validation library for this project) handles request-field validation. Declare constraints as `binding:"..."` struct tags on the request DTO and let `c.ShouldBindJSON(&req)` enforce them; on error return `response.Err(c, http.StatusBadRequest, err.Error())`. Do not add ad-hoc `len()`/empty checks for things a tag already covers.

- Common tags: `required`, `min`/`max` (length for strings/slices, value for numbers), `gt`/`gte`/`lt`/`lte`, `oneof`.
- **Slices/maps need `dive`** to validate their *elements*: `binding:"required,min=1,dive,required"` for `[]string`, and `binding:"required,min=1,dive"` for a slice of structs (without `dive`, element/struct-field tags are silently skipped). Directly-nested (non-slice) structs are validated automatically.
- **Numeric "0 is valid but missing is not"** can't be expressed with `required` (which rejects the zero value). Use a pointer field (`*float64`) plus an explicit `nil` check.
- **Booleans:** `required` on a plain `bool` rejects `false`. Use `*bool` + `binding:"required"`.

**Pagination convention:** all list endpoints accept `page` (1-based, default `1`) and `size` (default `20`, max `100`) query parameters. Invalid values (`page < 1`, `size < 1`, `size > 100`) return `400 Bad Request`. Compute `offset = (page-1) * size` and `limit = size`. Use the shared `parsePagination` helper in `handler/pagination.go`.

- **DB-backed list endpoints:** use a single SQL query with `LIMIT $n OFFSET $m` and `COUNT(*) OVER() AS total` (window function) to return both the page of rows and the total count in one round-trip.
- **External-API list endpoints:** fetch all results, then slice `all[offset:min(offset+size, total)]` client-side.
- Always populate all four `Meta` fields: `Total` (global count), `Page`, `Size`, `CurrentPageCount` (items in this page).

**Authorization on owned resources:** when a resource is addressed by ID and owned by a user, check ownership in the
use case. Return the resource's *not found* error (→ `404`, never `403`) when it is missing **or** owned by someone
else, so resource existence isn't leaked.
