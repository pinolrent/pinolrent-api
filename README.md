# Pinol Rent API

API for **peer-to-peer car rental**. Some users publish their cars (sellers), others book and pay for them (buyers). Built in **Go** with **SQLite**.

## Quick start

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api
```

For development (it uses default values and you do not need a `.env`):

```sh
make tools   # only the first time (installs air, govulncheck, golangci-lint into ~/go/bin — add it to your PATH)
make dev     # starts the server with dev defaults (JWT_SECRET auto-filled, never fails)
make run     # starts without defaults (fails without JWT_SECRET, like prod)
make watch   # starts with hot reload on edit (requires make tools)
```

## Endpoints

| Method | Path | Needs login? | What it does | Doc |
|--------|------|--------------|--------------|-----|
| GET | `/health` | no | See whether the server and the database are fine | [auth](docs/api/auth.md) |
| POST | `/auth/register` | no | Create an account (with a phone: also a seller) | [auth](docs/api/auth.md) |
| POST | `/auth/become-seller` | yes | Enable seller mode on your account | [auth](docs/api/auth.md) |
| POST | `/auth/login` | no | Log in and get access (15 min) + refresh (7 days) | [auth](docs/api/auth.md) |
| POST | `/auth/refresh` | no | Renew the pair with a single-use refresh token | [auth](docs/api/auth.md) |
| GET | `/auth/me` | yes | See your own profile | [auth](docs/api/auth.md) |
| PATCH | `/auth/me` | yes | Fix your contact phone | [auth](docs/api/auth.md) |
| PATCH | `/auth/password` | yes | Change your password (closes all your sessions) | [auth](docs/api/auth.md) |
| POST | `/auth/logout` | yes | Log out (invalidates your current token) | [auth](docs/api/auth.md) |
| GET | `/cars` | no | See available cars (you can filter by dates or seller) | [cars](docs/api/cars.md) |
| GET | `/cars/{id}` | no | See the detail of a car | [cars](docs/api/cars.md) |
| GET | `/cars/{id}/contact` | yes | Seller's WhatsApp link (to coordinate) | [cars](docs/api/cars.md) |
| GET · POST | `/seller/cars` | seller | See your cars and add a new one | [cars](docs/api/cars.md) |
| PATCH | `/seller/cars/{id}` | seller | Edit name, price or photo; activate/deactivate | [cars](docs/api/cars.md) |
| DELETE | `/seller/cars/{id}` | seller | Delete a car that never had reservations | [cars](docs/api/cars.md) |
| POST | `/reservations` | yes | Book a car | [reservations](docs/api/reservations.md) |
| GET | `/reservations` · `/reservations/{id}` | yes | See your reservations | [reservations](docs/api/reservations.md) |
| PATCH | `/reservations/{id}/cancel` | yes | Cancel one of your reservations (only if you have not paid yet) | [reservations](docs/api/reservations.md) |
| POST | `/reservations/{id}/payment` | yes | Pay for a reservation (`pos` or `cash`) | [payments](docs/api/payments.md) |
| GET | `/seller/reservations` | seller | See the reservations of your cars | [payments](docs/api/payments.md) |
| PATCH | `/seller/reservations/{id}/accept` | seller | Accept a pending reservation of your cars | [payments](docs/api/payments.md) |
| PATCH | `/seller/reservations/{id}/reject` | seller | Reject a pending reservation (its pending payment is rejected too) | [payments](docs/api/payments.md) |
| GET | `/notifications` | yes | See your own notifications | [notifications](docs/api/notifications.md) |
| PATCH | `/notifications/{id}/read` | yes | Mark one of your notifications as read | [notifications](docs/api/notifications.md) |
| PATCH | `/admin/reservations/{id}/confirm` | admin | Confirm an accepted reservation and approve its payment | [admin](docs/api/admin.md) |
| PATCH | `/admin/reservations/{id}/request-correction` | admin | Reject the payment so the buyer uploads a new proof | [admin](docs/api/admin.md) |
| GET | `/admin/users` · `/admin/cars` · `/admin/reservations` · `/admin/payments` · `/admin/notifications` · `/admin/stats` · `/admin/audit` | admin | Operate and inspect the platform | [admin](docs/api/admin.md) |
| POST | `/uploads` | yes | Upload an image (jpg/png/webp, 5 MB) and get its local URL; jpg/png are re-encoded without metadata | [uploads](docs/api/uploads.md) |
| GET | `/uploads/` | no | Serve an uploaded image (subtree, basename only) | [uploads](docs/api/uploads.md) |

If you try to read or touch something that is not yours, the API answers `404` as if it did not exist.

## Roles

- **Buyer:** books cars, pays and sees its reservations.
- **Seller:** publishes its cars and accepts or rejects the pending reservations of its cars. Each seller only sees its own. It registers with a mandatory phone: that is the number buyers use to reach it (WhatsApp).
- **Admin:** confirms accepted reservations (approving their payment), operates any car or account, and inspects the platform. Granted from the `ADMIN_EMAILS` allow-list only.

## Documentation

- **[API reference](docs/api/00-general.md)** — how to use the API, authentication and the detail of every endpoint.
- **[Configuration](docs/configuration.md)** — environment variables and startup.
- **[Architecture](docs/architecture.md)** — schema, reservation states and business rules.
- **[Deployment](docs/deployment.md)** — operating constraints, backups and restoration.

## Stack and basic rules

- **Go 1.26.9**, `net/http` without a framework, **SQLite** (`modernc.org/sqlite`) + `goose` migrations.
- Auth with **JWT HS256** (access 15 min + refresh 7 days with rotation) and **bcrypt** for passwords.
- `price_per_day` is in **cents** (e.g. 45000 = $450). Dates as `YYYY-MM-DD`.
- Every request with a body cannot exceed **1 MB**. JSON with unknown fields is an error.
- Everything under `/auth/` plus `GET /cars/{id}/contact` limited to **30 attempts per minute per IP**; writes (`POST`, `PATCH` and `DELETE` outside `/auth/`) to **120 per minute** (burst 60). CORS open by default (can be closed with `CORS_ALLOWED_ORIGINS`; with `ENV=prod` it does not allow `*`).
- Listings paginated with `limit`/`offset` (default 50, max 200, `offset` max 10000). Reservations of at most **30 days**.
- `GET /health` reports the version of the binary (`make build` injects it).