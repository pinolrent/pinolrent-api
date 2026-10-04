# Configuration

## Environment variables

| Variable | Default value | Required? | What it does |
|----------|---------------|-----------|--------------|
| `PORT` | `8080` | no | Port the server listens on |
| `DATABASE_URL` | `pinolrent.db` | no | Where the SQLite file lives |
| `JWT_SECRET` | — | **yes** | Secret used to sign the tokens (minimum 32 characters, with entropy: at least 16 distinct bytes) |
| `CORS_ALLOWED_ORIGINS` | `*` | no | Which origins may call the API, comma-separated. `*` = all of them |
| `ENV` | `dev` | no | Environment: `dev` (default) or `prod`/`production`. In prod `CORS_ALLOWED_ORIGINS=*` is rejected |
| `UPLOAD_DIR` | `uploads` | no | Directory where `POST /uploads` stores the images. Created on startup; must not be empty |
| `UPLOAD_MAX_TOTAL_MB` | `1024` | no | Total cap for `UPLOAD_DIR` in MB; `0` = no limit. Once exceeded, `POST /uploads` answers `507` |
| `TRUSTED_PROXY_CIDRS` | — (empty) | no | Networks whose `X-Forwarded-For`/`X-Real-IP` are believed, comma-separated (e.g. `172.18.0.0/16`). Empty = only a loopback proxy is trusted |
| `ADMIN_EMAILS` | — (empty) | no | Allow-list of accounts with the `admin` role, comma-separated (e.g. `admin@pinolrent.cl`). Empty = the installation has no administrator. A malformed entry prevents startup |

The priority order is: **shell variables > `.env` > default values**. An empty variable is
not ignored: it overrides the default with `""` and usually fails `Validate`
(the only exception is `scripts/dev.sh`, which treats empty values as unset
before starting the server).

### If the secret is missing, it does not start

```sh
$ go run ./cmd/api
# ERROR: missing required env vars: JWT_SECRET
```

```sh
$ JWT_SECRET=short go run ./cmd/api
# ERROR: JWT_SECRET must be at least 32 bytes (got 5)
```

```sh
$ JWT_SECRET=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa go run ./cmd/api
# ERROR: JWT_SECRET has too little entropy (only 1 unique bytes, want >= 16)
```

This is validated by `Config.Validate` in `internal/config/config.go`. A short or repetitive secret with HS256 is easy to break once someone gets hold of a token, which is why at least 32 characters with 16 distinct bytes are required.

### How to generate a secret

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
```

## Starting in production

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api   # or the binary built with make build
```

### `ENV` and `PORT`

- `ENV` defaults to `dev`. With `ENV=prod` or `ENV=production`, `CORS_ALLOWED_ORIGINS=*` is rejected.
- `PORT` must be `1..65535` or the server does not start.
- A malformed `.env` (e.g. a line without `=`) prevents startup — the error is printed in the log.

### Behind a reverse proxy

The per-IP rate limit and the logs need the real client IP, so the app only reads `X-Forwarded-For` / `X-Real-IP` when the request comes from a proxy it trusts:

- **Loopback proxy** (nginx or Caddy on the same machine): always trusted, nothing to configure.
- **Proxy on another network** (containers, any PaaS): that network must be declared in `TRUSTED_PROXY_CIDRS`. Otherwise every client shares the same limit counter (30 logins per minute across all of them) and the logs record the proxy IP.

```sh
export TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

When the peer is trusted, the `X-Forwarded-For` chain is walked **right to left** and the first value that is not itself a trusted proxy is taken. That way, if a client sends a made-up header, it cannot pick its own bucket: the address our proxy added stays at the end of the chain. The exact range comes from the proxy network (for example `docker network inspect proxy --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'`).

## Admin role

The only way to have an administrator is `ADMIN_EMAILS`. It is an email allow-list and the **only** source of the `admin` role: there is no endpoint that grants it or takes it away.

On startup the server syncs the role against the list —granting `admin` to the listed accounts that do not have it yet and revoking it from everyone else— inside a transaction. If the list is empty, it revokes everything. That is why **removing an email from the variable and restarting already takes the access away**, without touching the database by hand.

A new account with an email from the list gets the `admin` role in the same `POST /auth/register`, with no need to restart. The role is additive: the account is still a buyer (and a seller if it has a `phone`).

An administrator cannot suspend itself (`PATCH /admin/users/{id}` answers `400`), so the platform is never left with nobody able to undo the suspension.

The moderation actions (suspending, granting or revoking seller, editing or deleting a car) are written to `admin_audit_log` inside the same transaction as the change, and are read back with `GET /admin/audit`.

Accounts are created with `POST /auth/register` (without `phone` = buyer only, with `phone` = buyer + seller) and a buyer can upgrade to seller with `POST /auth/become-seller`.

What happens on startup:

1. Reads `.env` if it exists (whatever is already in the shell wins). If it is malformed, it shuts down.
2. Validates `JWT_SECRET` — if it is missing or short, it shuts down. Validates `PORT` and `CORS_ALLOWED_ORIGINS`/`ENV`.
3. Opens SQLite with WAL and applies the pending migrations (they are recorded in `goose_db_version`, they never delete data). For `:memory:` it uses a single connection, otherwise up to 8 (with `MaxIdleTime` 5 min / `MaxLifetime` 30 min). If the database is busy, it retries with backoff.
4. Syncs the `admin` role against `ADMIN_EMAILS` (if it comes empty, it revokes every administrator).
5. Starts the HTTP server and waits for `SIGINT`/`SIGTERM` to shut down cleanly (up to 10 s).

## Development

With `make dev` you do not need to create a `.env`: `scripts/dev.sh` sets the defaults (`JWT_SECRET=dev-secret-not-for-production-32b`, `DATABASE_URL=dev.db`) and starts with `go run`. If you run `make run` or `go run ./cmd/api` directly without variables, it does ask for the secret (that is the real production behaviour). `scripts/dev.sh` also rejects `CORS_ALLOWED_ORIGINS=*` when `ENV=prod`.

You can copy `.env.example` to `.env` to change those values.

> `dev.db` and its files (`-wal`, `-shm`) are in `.gitignore`: do not commit them.