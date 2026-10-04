# How to use the API

In development the API lives at `http://localhost:8080` (change it with `PORT`).

## Format

- Everything is **JSON** (`Content-Type: application/json`).
- Dates as `YYYY-MM-DD` text (compared in UTC).
- `price_per_day` is an integer in **cents**.

## How to authenticate

Protected routes need:

```
Authorization: Bearer <token>
```

You get the token from `POST /auth/login`: the access token lasts **15 min** and the refresh token **7 days** (renewed with `POST /auth/refresh`).

Roles (one account per email, with membership in 1–3 roles):

- **`buyer`**: books, pays and sees its reservations. Every account is born at least a buyer.
- **`seller`**: publishes its cars and confirms reservations of its cars. **Requires a phone**, because that is the number buyers use to reach it. You get it by registering with `phone` or later with `POST /auth/become-seller`.
- **`admin`**: manages accounts, cars, reservations and payments across the whole platform. Only obtainable through the `ADMIN_EMAILS` allow-list; there is no endpoint that grants it. See [`admin.md`](admin.md).

There is no "seller-only" account: a seller also books as a buyer with the same token. The frontend derives the Buy | Sell switch from `GET /auth/me` → `roles`.

If something fails during login:

| Status | Message | When it happens |
|--------|---------|-----------------|
| `401` | `missing bearer token` | You did not send the header, or sent it without `Bearer` |
| `401` | `invalid or expired token` | Forged, expired or revoked token |
| `401` | `user not found` | The user behind the token no longer exists |
| `403` | `insufficient permissions` | Valid token but no permission for that route |
| `403` | `account suspended` | The account was suspended by an administrator |

## How errors are returned

Always like this:

```json
{"error":"message"}
```

- `400` for bad input or invalid JSON.
- `409` for collisions (dates taken, a status that does not allow the action, duplicate payment). A duplicate email on registration deliberately answers the same `201 {"email":"..."}` to avoid revealing which emails exist.
- `404` to hide what is not yours: if it is not yours, it answers as if it did not exist.

## Limits

- **Max body 1 MB** (strict JSON: sending fields that do not exist or broken JSON → `400 {"error":"invalid JSON body"}`; going over the size → `413`). `POST /uploads` accepts up to **5 MB** per image (`jpg`/`png`/`webp`); `jpg`/`png` are re-encoded without metadata and cannot declare more than 50 megapixels.
- **Per-IP limit**: `/auth/*` → **30 per minute** (burst 30); writes (`POST`, `PATCH` and `DELETE` outside `/auth/`, e.g. `POST /reservations`, `POST /seller/cars`, `POST /uploads`, `PATCH /reservations/*/cancel`) → **120 per minute** (burst 60). Going over → `429 {"error":"too many requests"}` with header `Retry-After: 60`. `X-Forwarded-For` / `X-Real-IP` are only taken into account when the connection comes from a trusted proxy (loopback, or the networks listed in `TRUSTED_PROXY_CIDRS`); otherwise the connection IP counts.
- **CORS** open to everyone by default (`CORS_ALLOWED_ORIGINS=*`), rejected with `ENV=prod` or `production`. In production set your origins comma-separated, e.g. `CORS_ALLOWED_ORIGINS=https://app.example.com`. `OPTIONS` preflights answer `204`; if the origin is not allowed, the response carries no `Access-Control-Allow-Origin` and the browser blocks it. The allowed methods come from the routes that exist (`GET`, `POST`, `PATCH`, `DELETE` and `OPTIONS` today), and the headers are `Authorization` and `Content-Type`. A trailing `/` in the origin is tolerated.

## Pagination

The listings (`GET /cars`, `GET /seller/cars`, `GET /reservations`, `GET /seller/reservations`) accept:

| Parameter | Default | Rules |
|-----------|---------|-------|
| `limit` | `50` | `1..200`; otherwise → `400 "invalid limit"` |
| `offset` | `0` | `0..10000`; otherwise → `400 "invalid offset"` |

They return a **plain array**. To know whether there is more, ask for `limit+1` and see whether one extra item came back.

The admin listings (`/admin/users`, `/admin/cars`, `/admin/reservations`, `/admin/payments`, `/admin/audit`) return an **object** with `items`, `total`, `limit` and `offset`.

## Endpoints

| Method | Path | Login? | Doc |
|--------|------|--------|-----|
| GET | `/health` | no | [auth](auth.md) |
| POST | `/auth/register` | no | [auth](auth.md) |
| POST | `/auth/become-seller` | yes | [auth](auth.md) |
| POST | `/auth/login` | no | [auth](auth.md) |
| POST | `/auth/refresh` | no | [auth](auth.md) |
| GET | `/auth/me` | yes | [auth](auth.md) |
| PATCH | `/auth/me` | yes | [auth](auth.md) |
| PATCH | `/auth/password` | yes | [auth](auth.md) |
| POST | `/auth/logout` | yes | [auth](auth.md) |
| GET | `/cars` | no | [cars](cars.md) |
| GET | `/cars/{id}` | no | [cars](cars.md) |
| GET | `/cars/{id}/contact` | yes | [cars](cars.md) |
| GET | `/seller/cars` | seller | [cars](cars.md) |
| POST | `/seller/cars` | seller | [cars](cars.md) |
| PATCH | `/seller/cars/{id}` | seller | [cars](cars.md) |
| DELETE | `/seller/cars/{id}` | seller | [cars](cars.md) |
| POST | `/reservations` | yes | [reservations](reservations.md) |
| GET | `/reservations` | yes | [reservations](reservations.md) |
| GET | `/reservations/{id}` | yes | [reservations](reservations.md) |
| PATCH | `/reservations/{id}/cancel` | yes | [reservations](reservations.md) |
| POST | `/reservations/{id}/payment` | yes | [payments](payments.md) |
| GET | `/notifications` | yes | [notifications](notifications.md) |
| PATCH | `/notifications/{id}/read` | yes | [notifications](notifications.md) |
| GET | `/seller/reservations` | seller | [payments](payments.md) |
| PATCH | `/seller/reservations/{id}/accept` | seller | [payments](payments.md) |
| PATCH | `/seller/reservations/{id}/reject` | seller | [payments](payments.md) |
| POST | `/uploads` | yes | [uploads](uploads.md) |
| GET | `/uploads/{name}` | no | [uploads](uploads.md) |
| GET | `/admin/users` | admin | [admin](admin.md) |
| GET | `/admin/users/{id}` | admin | [admin](admin.md) |
| PATCH | `/admin/users/{id}` | admin | [admin](admin.md) |
| PATCH | `/admin/users/{id}/roles` | admin | [admin](admin.md) |
| GET | `/admin/cars` | admin | [admin](admin.md) |
| PATCH | `/admin/cars/{id}` | admin | [admin](admin.md) |
| DELETE | `/admin/cars/{id}` | admin | [admin](admin.md) |
| GET | `/admin/reservations` | admin | [admin](admin.md) |
| PATCH | `/admin/reservations/{id}/confirm` | admin | [admin](admin.md) |
| PATCH | `/admin/reservations/{id}/request-correction` | admin | [admin](admin.md) |
| GET | `/admin/payments` | admin | [admin](admin.md) |
| GET | `/admin/notifications` | admin | [admin](admin.md) |
| GET | `/admin/stats` | admin | [admin](admin.md) |
| GET | `/admin/audit` | admin | [admin](admin.md) |

## Shapes the API returns

**User (own profile):**

```json
{"id":3,"email":"demo@example.com","roles":["buyer"],"phone":"+56912345678"}
```

`phone` comes back empty if you did not set one. It is normalized to E.164 (`+56912345678`).

**Car:**

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true}
```

`photo_url` is omitted when empty. `owner_id` is who published it.

**Reservation (with its car and its payment when they exist):**

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{ "...": "car" },
  "payment":{ "...": "payment" }
}
```

`payment` does not show up until it is paid.

**Payment:**

```json
{"id":1,"reservation_id":1,"method":"pos","status":"pending","proof_url":"https://..."}
```

`proof_url` is omitted when empty.