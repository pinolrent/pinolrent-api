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

| Method | Path | Needs login? | What it does |
|--------|------|--------------|--------------|
| GET | `/health` | no | See whether the server and the database are fine |
| POST | `/auth/register` | no | Create an account (with a phone: also a seller) |
| POST | `/auth/become-seller` | yes | Enable seller mode on your account |
| POST | `/auth/login` | no | Log in and get access (15 min) + refresh (7 days) |
| POST | `/auth/refresh` | no | Renew the pair with a single-use refresh token |
| GET | `/auth/me` | yes | See your own profile |
| PATCH | `/auth/me` | yes | Fix your contact phone |
| PATCH | `/auth/password` | yes | Change your password (closes all your sessions) |
| POST | `/auth/logout` | yes | Log out (invalidates your current token) |
| GET | `/cars` | no | See available cars (you can filter by dates or seller) |
| GET | `/cars/{id}` | no | See the detail of a car |
| GET | `/cars/{id}/contact` | yes | Seller's WhatsApp link (to coordinate) |
| GET · POST | `/seller/cars` | seller | See your cars and add a new one |
| PATCH | `/seller/cars/{id}` | seller | Edit name, price or photo; activate/deactivate |
| DELETE | `/seller/cars/{id}` | seller | Delete a car that never had reservations |
| POST | `/reservations` | yes | Book a car |
| GET | `/reservations` · `/reservations/{id}` | yes | See your reservations |
| PATCH | `/reservations/{id}/cancel` | yes | Cancel one of your reservations (only if you have not paid yet) |
| POST | `/reservations/{id}/payment` | yes | Pay for a reservation (`pos` or `cash`) |
| GET | `/seller/reservations` | seller | See the reservations of your cars |
| PATCH | `/seller/reservations/{id}/confirm` | seller | Confirm a reservation and approve its payment |
| PATCH | `/seller/reservations/{id}/reject` | seller | Reject the payment, cancel the reservation and release the dates |
| POST | `/uploads` | yes | Upload an image (jpg/png/webp, 5 MB) and get its local URL; jpg/png are re-encoded without metadata |
| GET | `/uploads/{name}` | no | See an uploaded image |

If you try to read or touch something that is not yours, the API answers `404` as if it did not exist.

## Roles

- **Buyer:** books cars, pays and sees its reservations.
- **Seller:** publishes its cars and confirms the reservations of its cars. Each seller only sees its own. It registers with a mandatory phone: that is the number buyers use to reach it (WhatsApp).

## Documentation

- **[API reference](docs/api/00-general.md)** — how to use the API, authentication and the detail of every endpoint.
- **[Configuration](docs/configuration.md)** — environment variables and startup.
- **[Architecture](docs/architecture.md)** — schema, reservation states and business rules.
- **[Deployment](docs/deployment.md)** — operating constraints, backups and restoration.

## Stack and basic rules

- **Go 1.26.6**, `net/http` without a framework, **SQLite** (`modernc.org/sqlite`) + `goose` migrations.
- Auth with **JWT HS256** (access 15 min + refresh 7 days with rotation) and **bcrypt** for passwords.
- `price_per_day` is in **cents** (e.g. 45000 = $450). Dates as `YYYY-MM-DD`.
- Every request with a body cannot exceed **1 MB**. JSON with unknown fields is an error.
- Login and registration limited to **30 attempts per minute per IP**; writes (`POST`, `PATCH` and `DELETE` outside `/auth/`) to **120 per minute** (burst 60). CORS open by default (can be closed with `CORS_ALLOWED_ORIGINS`; with `ENV=prod` it does not allow `*`).
- Listings paginated with `limit`/`offset` (default 50, max 200, `offset` max 10000). Reservations of at most **30 days**.
- `GET /health` reports the version of the binary (`make build` injects it).