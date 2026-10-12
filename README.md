# Pinol Rent API

API for **peer-to-peer car rental**. Some users publish their cars (sellers), others book and pay for them (buyers). Built in **Go** with **SQLite**.

## Quick start

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api
curl -fsS http://localhost:8080/health
```

For development (it uses default values and you do not need a `.env`):

```sh
make tools   # only the first time (installs air, govulncheck, golangci-lint into ~/go/bin — add it to your PATH)
make dev     # starts the server with dev defaults (JWT_SECRET auto-filled, never fails)
make run     # starts without defaults (fails without JWT_SECRET, like prod)
make watch   # starts with hot reload on edit (requires make tools)
make demo    # end-to-end smoke on :8132 with an ephemeral DB (mirrors bruno/pinolrent-api/flow/)
```

## API overview

Condensed map. The full reference lives in `docs/api/` — start there for
status codes, error messages and examples.

| Area | Start with | Doc |
|------|------------|-----|
| Health | `GET /health` | [auth](docs/api/auth.md) |
| Auth | `POST /auth/register`, `POST /auth/login`, `GET /auth/me` | [auth](docs/api/auth.md) |
| Catalog | `GET /cars`, `GET /cars/{id}` | [cars](docs/api/cars.md) |
| Seller cars | `GET /seller/cars`, `POST /seller/cars` | [cars](docs/api/cars.md) |
| Reservations | `POST /reservations`, `GET /reservations` | [reservations](docs/api/reservations.md) |
| Payments | `POST /reservations/{id}/payment`, seller accept/reject | [payments](docs/api/payments.md) |
| Notifications | `GET /notifications` | [notifications](docs/api/notifications.md) |
| Uploads | `POST /uploads` | [uploads](docs/api/uploads.md) |
| Admin | `GET /admin/stats`, `PATCH /admin/reservations/{id}/confirm` | [admin](docs/api/admin.md) |

If you try to read or touch something that is not yours, the API answers `404` as if it did not exist.

## Roles

- **Buyer:** books cars, pays and sees its reservations.
- **Seller:** publishes its cars and accepts or rejects the pending reservations of its cars. Requires a phone (WhatsApp contact).
- **Admin:** confirms accepted reservations and inspects the platform. Granted from the `ADMIN_EMAILS` allow-list only.

See [How to use the API](docs/api/00-general.md) for the role model.

## Documentation

- **[API reference](docs/api/00-general.md)** — how to use the API, authentication and the detail of every endpoint.
- **[Configuration](docs/configuration.md)** — environment variables and startup.
- **[Architecture](docs/architecture.md)** — schema, reservation states and business rules.
- **[Deployment](docs/deployment.md)** — operating constraints, backups and restoration.
- **[Bruno collection](bruno/pinolrent-api/)** — chained requests in `flow/` (`seq` order), same buyer/seller flow as `scripts/demo.sh`.

## Stack and basic rules

- **Go 1.26.9**, `net/http` without a framework, **SQLite** (`modernc.org/sqlite`) + `goose` migrations.
- Auth with **JWT HS256** (access 15 min + refresh 7 days with rotation) and **bcrypt** for passwords.
- `price_per_day` is in **cents** (e.g. 45000 = $450). Dates as `YYYY-MM-DD`.
- See the [API reference](docs/api/00-general.md) for body limits, rate limits, pagination and CORS.