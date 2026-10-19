# How to use the API

In development the API lives at `http://localhost:8080` (change it with `PORT`).

## Docs map

- [Auth](auth.md) — registration, login, refresh, logout, profile, password, JWT claims.
- [Cars](cars.md) — public catalog plus the seller CRUD.
- [Reservations](reservations.md) — booking, buyer views, cancel, seller listing.
- [Payments](payments.md) — buyer payment plus the seller accept/reject step.
- [Notifications](notifications.md) — in-app fan-out per transition, audit cross-table.
- [Uploads](uploads.md) — image upload, re-encoding rules, quota, serve path.
- [Admin](admin.md) — users, cars, reservations, payments, stats, audit.
- [Configuration](../configuration.md) — environment variables and startup order.
- [Architecture](../architecture.md) — schema, state machine, blocking levels.
- [Deployment](../deployment.md) — single-instance constraints, backups, restore.

The same flow is executable: `scripts/demo.sh` (`make demo`, ephemeral DB on
`:8132`) and the chained Bruno requests in `bruno/pinolrent-api/flow/` (`seq`
order, tokens and IDs via variables).

## End-to-end flow

```sh
BASE=http://localhost:8080

# 1. Seller account (phone grants the seller role) and login.
curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123","phone":"+50581234567"}'
SELLER_TOKEN=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123"}' | jq -r .token)

# 2. Buyer account and login.
curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}'
BUYER_TOKEN=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}' | jq -r .token)

# 3. Seller publishes a car.
CAR_ID=$(curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $SELLER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Toyota Yaris","price_per_day":45000}' | jq -r .id)

# 4. Buyer books it.
RES_ID=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $BUYER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$CAR_ID,\"start_date\":\"2026-11-01\",\"end_date\":\"2026-11-05\"}" | jq -r .id)

# 5. Buyer records the payment.
curl -s -X POST "$BASE/reservations/$RES_ID/payment" -H "Authorization: Bearer $BUYER_TOKEN" \
  -H 'Content-Type: application/json' -d '{"method":"pos"}'

# 6. Seller accepts; admin confirms (needs an ADMIN_EMAILS account).
curl -s -X PATCH "$BASE/seller/reservations/$RES_ID/accept" \
  -H "Authorization: Bearer $SELLER_TOKEN"
curl -s -X PATCH "$BASE/admin/reservations/$RES_ID/confirm" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Roles come from `GET /auth/me` → `roles`: the frontend derives its Buy | Sell
switch from there.

## Format

- Everything is **JSON** (`Content-Type: application/json`).
- Dates as `YYYY-MM-DD` text. "Today" is the calendar day in the business time zone (`BUSINESS_TIMEZONE`, default `America/Managua`).
- `price_per_day` is an integer in **cents**.

## How to authenticate

Protected routes need:

```
Authorization: Bearer <token>
```

You get the token from `POST /auth/login`: the access token lasts **15 min** and the refresh token **7 days** (renewed with `POST /auth/refresh`).

Roles (one account per email, with membership in 1–3 roles):

- **`buyer`**: books, pays and sees its reservations. Every account is born at least a buyer.
- **`seller`**: publishes its cars and accepts or rejects reservation requests for them (confirmation itself is the administrator's call). **Requires a phone**, because that is the number buyers use to reach it. You get it by registering with `phone` or later with `POST /auth/become-seller`.
- **`admin`**: manages accounts, cars, reservations and payments across the whole platform. Only obtainable through the `ADMIN_EMAILS` allow-list; there is no endpoint that grants it. See [`admin.md`](admin.md).

There is no "seller-only" account: a seller also books as a buyer with the same token. The frontend derives the Buy | Sell switch from `GET /auth/me` → `roles`.

Authorization is resolved against the database on every request, not against the token: the `roles` inside the JWT only prove the token was issued to a real account, while roles, suspension and session validity are re-read from the `users` row. Role changes and suspensions therefore take effect on the next request without re-login. `GET /cars/{id}/contact` needs any login on purpose (buyers must reach the seller to coordinate); it is rate-limited strictly because it exposes the seller's phone.

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
- **CORS** open to everyone by default (`CORS_ALLOWED_ORIGINS=*`), rejected with `ENV=prod` or `production` (a `*` entry is rejected even when mixed with real origins). In production set your origins comma-separated, e.g. `CORS_ALLOWED_ORIGINS=https://app.example.com`. `OPTIONS` preflights answer `204`; if the origin is not allowed, the response carries no `Access-Control-Allow-Origin` and the browser blocks it. The allowed methods come from the routes that exist (`GET`, `POST`, `PATCH`, `DELETE` and `OPTIONS` today), and the headers are `Authorization` and `Content-Type`. A trailing `/` in the origin is tolerated.

## Troubleshooting

| Symptom | Likely cause | What to check |
|---------|--------------|---------------|
| `400 invalid JSON body` | Broken JSON or unknown fields (strict decoding) | Field names against the endpoint table; no trailing commas |
| `401 missing bearer token` | No `Authorization: Bearer <token>` header | Header present, `Bearer` prefix, token not truncated |
| `401 invalid or expired token` | Expired access token (15 min), rotated refresh token, logout, password change, reused refresh (kills all sessions) | Log in again; use the newest refresh token exactly once |
| `403 insufficient permissions` | Valid token, missing role (`seller` for `/seller/*`, `admin` for `/admin/*`) | `GET /auth/me` → `roles` |
| `403 account suspended` | Admin suspended the account | Admin reactivates via `PATCH /admin/users/{id}` |
| `404` on something that exists | Privacy rule: not yours reads as missing | Buyer sees own reservations; seller sees own cars/reservations |
| `409 car already reserved for the requested dates` | Overlapping `pending`/`accepted`/`confirmed` reservation | Change dates or pick another car |
| `409 reservation is not pending` / `is not accepted` | Wrong state for the transition | State table in [Architecture](../architecture.md) |
| `413` | JSON body over 1 MB, or image over 5 MB on `POST /uploads` | Shrink body; re-encode image |
| `415` | Non-image or non-jpg/png/webp upload (sniffed by content) | Convert to jpg/png/webp |
| `429 too many requests` | `/auth/*` over 30/min or writes over 120/min per IP; `Retry-After: 60` | Back off; behind a proxy check `TRUSTED_PROXY_CIDRS` in [Configuration](../configuration.md) |
| `503` on `/health` or writes | Database busy (`503` + `Retry-After` on writes; `degraded` on health) | Retry; never treat as `500` |
| `507 storage quota exceeded` | `UPLOAD_DIR` over `UPLOAD_MAX_TOTAL_MB` | Prune uploads or raise the quota |

Send `X-Request-Id` to correlate a failing call with its server log line.

## Pagination

The listings (`GET /cars`, `GET /seller/cars`, `GET /reservations`, `GET /seller/reservations`, `GET /notifications`) accept:

| Parameter | Default | Rules |
|-----------|---------|-------|
| `limit` | `50` | `1..200`; otherwise → `400 "invalid limit"` |
| `offset` | `0` | `0..10000`; otherwise → `400 "invalid offset"` |

They return a **plain array**. To know whether there is more, ask for `limit+1` and see whether one extra item came back.

The admin listings (`/admin/users`, `/admin/cars`, `/admin/reservations`, `/admin/payments`, `/admin/notifications`, `/admin/audit`) return an **object** with `items`, `total`, `limit` and `offset`.

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
| GET | `/uploads/` | no | [uploads](uploads.md) |
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
{"id":3,"email":"demo@example.com","roles":["buyer"],"phone":"+50581234567"}
```

`phone` comes back empty if you did not set one. It is normalized to E.164 (`+50581234567`).

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