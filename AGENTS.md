# AGENTS.md

## Project overview

PinolRent API — P2P car rental between private owners. Sellers publish cars, buyers reserve and pay.
Go 1.26.5, `net/http` stdlib, SQLite (`modernc.org/sqlite`), `goose` migrations, JWT HS256, bcrypt.

```
cmd/api/                  # server entrypoint, middleware wiring, graceful shutdown
internal/
  config/                 # env parsing and validation
  db/                     # SQLite open + embedded goose migrations
  auth/                   # password hashing, JWT sign/verify, logout revocation
  models/                 # domain types (User, Car, Reservation, Payment)
  handlers/               # routes and endpoint logic
  ratelimit/              # in-memory per-IP token bucket
scripts/                  # dev.sh, demo.sh
bruno/                    # Bruno API collection
```

Docs are in Spanish (`docs/`), code comments in English. Follow that convention.

## Setup

```sh
make tools   # one-time: installs air, govulncheck, golangci-lint to $(go env GOPATH)/bin
             # make sure $(go env GOPATH)/bin is in your PATH
make dev     # start server with dev defaults (JWT_SECRET auto-filled, no .env needed)
make run     # start without defaults — fails without JWT_SECRET, like prod
make watch   # hot-reload via air (requires make tools)
```

Required env: `JWT_SECRET` (min 32 bytes). Optional: `PORT` (default 8080, must be 1-65535),
`DATABASE_URL` (default `pinolrent.db`, dev uses `dev.db`), `CORS_ALLOWED_ORIGINS` (default `*`),
`ENV` (default `dev`; `prod`/`production` rejects `CORS_ALLOWED_ORIGINS=*`).

Priority: shell env > `.env` > defaults. A malformed `.env` is a hard error. See `.env.example`
and `docs/configuracion.md` for details.

## Build, test and lint

```sh
make build       # bin/pinolrent-api with version from git describe
make test        # go test -count=1 -timeout 180s ./...
make test-race   # CGO_ENABLED=1 -race -timeout 300s (needs gcc)
make cover       # coverage summary (180s, atomic)
make vuln        # govulncheck ./...
make vet         # go vet ./...
make lint        # golangci-lint run ./... (config: .golangci.yml v2, must be 0)
make fmt         # gofmt -w .
make tidy        # go mod tidy
make demo        # full E2E smoke on :8132 with ephemeral DB (36 checks)
make clean       # removes bin/, tmp/, air.log, dev.db
```

Before every commit, all of these must pass (no exceptions):

```sh
make fmt
go vet ./... 2>&1 | head   # or: make vet
test -z "$(gofmt -l .)"    # no unformatted files
go mod tidy && git diff --exit-code -- go.mod go.sum  # tidy
make lint                  # must be 0 (config: .golangci.yml v2)
make test                  # 180s
make vuln                  # govulncheck ./...
```

`make test-race` (300s, needs gcc) is not required per-commit — CI runs it. Run it locally before opening a PR if you touched concurrent code (`handlers`, `db`, `ratelimit`).

Fix every failure before committing. Never push with a red check that you could have caught locally.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every PR:
`go mod verify` → `gofmt` check → `go mod tidy` check → `make vet` → `make test` →
`make test-race` → `golangci-lint v2.13.2` → `govulncheck` → `make cover` → `make build` →
`make demo`. Keep `GOLANGCI_VERSION` in `Makefile` and the workflow in sync.

## Testing

- Tests live next to code as `*_test.go`, package `internal/auth`, `config`, `db`, `handlers`, `ratelimit`.
- Helpers in `internal/handlers/helpers_test.go`: `newTestAPI`, `doJSON`, `newSeller`, `registerBuyer`, `futureDate`.
  Use `futureDate(n)` for reservation dates — never hardcode calendar dates (they go stale).
- `internal/handlers/middleware_test.go` covers security headers, panic recovery, and edge cases.
- `scripts/demo.sh` is the E2E smoke — exercises the full buyer/seller flow with `curl`+`jq`.
- Bruno collection in `bruno/pinolrent-api/` mirrors all routes — keep it in sync when adding endpoints.
- Aim to add or update tests for every change.

## Migrations

SQL files in `internal/db/migrations/*.sql`, applied at startup via embedded `goose`. Only pending migrations run.
Never delete or reorder existing files — create a new one:

```sh
go run github.com/pressly/goose/v3/cmd/goose@latest create add_<name> sql
```

Move the generated file to `internal/db/migrations/` and edit `Up`/`Down`.

## Bruno collection

`bruno/pinolrent-api/` — requests must run in `seq` order (tokens and IDs chain via variables).
`collection.bru` defines `baseUrl` (default `http://localhost:8080`), `sellerToken`/`buyerToken`,
`carId`/`reservationId`. Update the collection when adding or changing endpoints.

## Commit and PR conventions

- Prefix: `feat:`, `fix:`, `docs:`, `build:`, `ci:`, `test:`, `style:`, `chore:`
- Keep commits focused — one area per commit. Split tooling/docs/CI/tests into separate commits.
- Commit body: describe what changed, no conversational context or chat references.
- No `Co-authored-by` trailer.
- Do not push directly to `main` — work on a branch and open a PR.

## Deployment notes

- `JWT_SECRET` must be set in prod (min 32 bytes, generate with `openssl rand -base64 32`).
- Set `ENV=prod` and `CORS_ALLOWED_ORIGINS` to your frontend origin(s).
- `PORT` must be a valid port. `.env` errors fail fast.
- The server handles `SIGINT`/`SIGTERM` with a 10s graceful shutdown.
