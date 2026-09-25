# Configuration

Read this before adding, renaming or removing a config key or environment variable.

## Environment variables

All config values can be overridden at runtime via environment variables. The naming rule is:
**config path → uppercase, dots replaced by underscores** (e.g. `auth.google.client_id` → `AUTH_GOOGLE_CLIENT_ID`).

| Env var                                 | `config.yaml` key                       | Default |
|-----------------------------------------|-----------------------------------------|---------|
| `SERVER_PORT`                           | `server.port`                           | `8080`  |
| `LOG_LEVEL`                             | `log.level`                             | `info`  |
| `LOG_FORMAT`                            | `log.format`                            | `json`  |
| `DB_POSTGRES_DSN`                       | `db.postgres.dsn`                       | —       |
| `DB_MIGRATE_ENABLED`                    | `db.migrate.enabled`                    | `true`  |
| `AUTH_GOOGLE_CLIENT_ID`                 | `auth.google.client_id`                 | —       |
| `AUTH_GOOGLE_CLIENT_SECRET`             | `auth.google.client_secret`             | —       |
| `AUTH_GOOGLE_REDIRECT_URL`              | `auth.google.redirect_url`              | —       |
| `AUTH_JWT_SECRET`                       | `auth.jwt.secret`                       | —       |
| `THIRD_PARTY_API_FMP_API_KEY`           | `third_party_api.fmp.api_key`           | —       |
| `THIRD_PARTY_API_GROQ_API_KEY`          | `third_party_api.groq.api_key`          | —       |
| `REDIS_ADDR`                            | `redis.addr`                            | —       |
| `REDIS_PASSWORD`                        | `redis.password`                        | —       |
| `REDIS_DB`                              | `redis.db`                              | `0`     |

Env vars take precedence over `config.yaml`. In production, set secrets via env vars and omit them from `config.yaml`
entirely. Feature-specific config keys (e.g. per-feature limits) are documented in the relevant feature doc.

## Config file rules

**Config file rule:** whenever `config.yaml` is modified (keys added, renamed, or removed), `config.yaml.example` must be updated in the same change. `config.yaml` is git-ignored; `config.yaml.example` is the committed reference that other developers copy to get started.

**NEVER put real secrets in `config.yaml.example`** — no real API keys, passwords, tokens, or credentials of any kind. Use descriptive placeholders only (e.g. `"YOUR_ALPHA_VANTAGE_API_KEY"`, `"YOUR_JWT_SECRET_CHANGE_ME"`). `config.yaml.example` is committed to the repository and publicly visible.
