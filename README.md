# gofrmrkt — Gophermart Loyalty System

A loyalty-points accrual and redemption service ("накопительная система лояльности")
built as the diploma project for the Yandex Practicum Go course. Users register
orders, the service polls an external accrual system for reward calculations, and
users can spend accumulated points against future orders.

## Architecture

- **HTTP layer**: [chi](https://github.com/go-chi/chi) router, JWT-based auth
  (cookie), gzip request/response compression, structured request logging (zap).
- **Storage**: PostgreSQL via [pgx](https://github.com/jackc/pgx), schema managed
  by [golang-migrate](https://github.com/golang-migrate/migrate) (`migrations/`).
- **Accrual integration**: a background worker (`internal/worker`) polls the
  external accrual system for orders awaiting calculation, respecting its rate
  limiting (`429`/`Retry-After`).
- **Resilience**: transient database errors (connection loss, timeouts) are
  retried with backoff (`internal/service/retry`); withdrawals are idempotent
  against retried attempts.
- **Lifecycle**: graceful shutdown on `SIGINT`/`SIGTERM` — stops accepting new
  HTTP requests, lets in-flight requests finish (up to a bounded timeout), stops
  the accrual worker, then closes the database connection.

## Running

```bash
go build -o gophermart ./cmd/gophermart
./gophermart -a localhost:8080 -d "postgres://user:pass@localhost:5432/gophermart" -r "http://localhost:8081"
```

### Configuration

Every setting can be provided as a flag or an environment variable (the
environment variable takes precedence if both are set):

| Flag | Env var                  | Required | Description                                   |
|------|---------------------------|----------|------------------------------------------------|
| `-a` | `RUN_ADDRESS`             | yes      | Address the HTTP server listens on             |
| `-d` | `DATABASE_URI`            | yes      | PostgreSQL connection string                   |
| `-r` | `ACCRUAL_SYSTEM_ADDRESS`  | yes      | Base URL of the external accrual system        |
| `-k` | `SECRET_KEY`              | no       | JWT signing secret. If omitted, a random key is generated at startup (tokens won't survive a restart, but the service never fails to start over a missing secret). |

Database migrations run automatically on startup.

## API

All authenticated endpoints expect the JWT issued by register/login, sent
back as the `token` cookie.

| Method | Path                          | Auth | Description                                  |
|--------|-------------------------------|------|-----------------------------------------------|
| POST   | `/api/user/register`          | —    | Register a new user; auto-authenticates on success |
| POST   | `/api/user/login`             | —    | Authenticate an existing user                |
| POST   | `/api/user/orders`            | yes  | Upload an order number for accrual calculation (Luhn-validated) |
| GET    | `/api/user/orders`            | yes  | List the user's uploaded orders, newest first |
| GET    | `/api/user/balance`           | yes  | Current point balance and total withdrawn    |
| POST   | `/api/user/balance/withdraw`  | yes  | Redeem points against an order number        |
| GET    | `/api/user/withdrawals`       | yes  | List the user's withdrawal history           |

See `SPECIFICATION.md` for the full request/response contract, status codes,
and the external accrual system's own API.

## Testing

```bash
go test ./...
```

Note: the CI pipeline (`.github/workflows/gophermart.yml`) currently runs the
official black-box `gophermarttest` suite and `go vet` (`statictest.yml`) —
neither executes this repository's own unit tests. Run `go test ./...`
locally before relying on it as a merge gate.