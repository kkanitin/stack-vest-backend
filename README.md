# StackVest Backend

Go REST API for the StackVest platform, built with [Gin](https://github.com/gin-gonic/gin) and MongoDB following Clean
Architecture principles.

## Requirements

- Go 1.21+
- MongoDB

## Getting Started

1. Copy the environment variables:
   ```bash
   cp .env.example .env
   ```

2. Run the server:
   ```bash
   go run main.go
   ```

The server starts on `:8080` by default.

## Environment Variables

| Variable         | Default | Description            |
|------------------|---------|------------------------|
| `SERVER_ADDRESS` | `:8080` | HTTP listen address    |
| `MONGO_URI`      | —       | MongoDB connection URI |

## Project Structure

```
internal/
  domain/             # Entities and repository/usecase interfaces
  usecase/            # Business logic
  repository/         # MongoDB implementations
  delivery/http/
    handler/          # Gin request handlers
    router/           # Route registration
    middleware/       # Gin middleware

pkg/
  config/             # Environment config
  database/           # MongoDB client setup
```

## Development

```bash
go build -o backend .   # build
go test ./...           # run tests
go vet ./...            # lint
```