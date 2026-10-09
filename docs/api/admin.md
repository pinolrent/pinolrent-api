# Admin

Platform administration endpoints. They all require a token from an account with
the `admin` role.

## How the `admin` role is obtained

Through the `ADMIN_EMAILS` environment variable, and nothing else. It is an
email allow-list and the server syncs it against the database on startup:

- listed accounts that do not have the role get it;
- accounts that have it and are not on the list lose it;
- an empty list revokes the role from everyone.

An account registering with an email from the list is born `admin`, without
waiting for a restart. **There is no endpoint to grant or to remove the `admin`
role** — that is deliberate: if there were, anyone with an admin token would
also be a way to stay an admin forever, and the allow-list would stop being the
source of truth.

See [`docs/configuration.md`](../configuration.md) for the details.

## Authorization

Every endpoint answers:

| Situation | Status |
|-----------|--------|
| No `Authorization: Bearer` | `401` |
| Invalid or expired token | `401` |
| Suspended account | `403` `account suspended` |
| Valid token without the `admin` role | `403` `insufficient permissions` |

An administrator is **not** an implicit seller: the `/seller/*` endpoints still
require the `seller` role. Administration goes through `/admin/*`.

Reads (`GET`) carry no rate limit; the ones that write (`PATCH`, `DELETE`) use
the standard per-IP bucket.

---

## `GET /admin/users`

Lists the accounts of the platform. Supports filters and pagination.

**Query:**

| Parameter | Type | Rules |
|-----------|------|-------|
| `q` | text | searches `email` (substring, case-insensitive) or the exact id |
| `role` | text | `buyer`, `seller` or `admin` |
| `limit` | number | 1 to 200; default 50 |
| `offset` | number | 0 to 10000; default 0 |

```json
{
  "items": [
    {"id": 1, "email": "admin@example.com", "roles": ["admin", "buyer"], "phone": ""},
    {"id": 2, "email": "seller@example.com", "phone": "+50581234567", "roles": ["buyer", "seller"]},
    {"id": 3, "email": "suspended@example.com", "roles": ["buyer"], "suspended_at": 1730000000}
  ],
  "total": 3,
  "limit": 50,
  "offset": 0
}
```

`phone` comes back empty if the account has none. `suspended_at` (Unix) only
shows up on suspended accounts. `roles` always comes back alphabetically sorted.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid limit` | `limit` is not a number or falls outside 1..200 |
| `400` | `invalid offset` | `offset` is negative or greater than 10000 |

---

## `GET /admin/users/{id}`

Detail of an account: id, email, phone, roles and `suspended_at` when it is
suspended. Same shape as an item of the listing.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid user id` | `{id}` is not an integer |
| `404` | `user not found` | The account does not exist |

---

## `PATCH /admin/users/{id}`

Suspends or reactivates an account. A suspended account gets `403 account
suspended` on **every** authenticated route, with a token that is still valid.
Reactivating it restores access without logging in again.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `suspended` | boolean | yes | `true` suspends, `false` reactivates |

```json
{"suspended": true}
```

**Answers** `200` with the updated state of the account.

The operation is idempotent: suspending an already suspended account returns
`200` without writing anything, neither in the database nor in the audit trail.

An administrator **cannot suspend itself** (`400 cannot suspend yourself`): it
would be a way to leave the platform with nobody able to undo the suspension.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid user id` | `{id}` is not an integer |
| `400` | `cannot suspend yourself` | `{id}` is the account making the call |
| `400` | `suspended is required` | `suspended` is missing from the body |
| `404` | `user not found` | The account does not exist |

---

## `PATCH /admin/users/{id}/roles`

Grants or revokes the `seller` role. It does not touch the `admin` role (see
above).

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `seller` | boolean | yes | `true` grants seller, `false` revokes it |

```json
{"seller": true}
```

**Answers** `200` with the resulting roles. It is idempotent: granting it to
someone who is already a seller returns `200` without writing.

Revoking the `seller` role does not delete the cars or the reservations of the
account: it only takes away access to `/seller/*`. It is blocked with `409`
while the account still has future reservations (`pending`, `accepted` or
`confirmed` ending today or later), because its buyers would be left with no
one to accept or reject: resolve those dates first.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid user id` | `{id}` is not an integer |
| `400` | `seller is required` | `seller` is missing from the body |
| `404` | `user not found` | The account does not exist |
| `409` | `user has no phone number` | Granting seller to an account without a phone (the role promises buyers a contact number) |
| `409` | `user has future reservations, cannot revoke seller` | Revoking while the account has reservations ending today or later |

---

## `GET /admin/cars`

Lists every car on the platform, **including the inactive ones**. It applies
neither the owner filter nor the date availability filter of the public view: it
is an administrative view.

**Query:** `q` (name or id), `owner_id`, `active` (`true`/`false`), `limit`,
`offset`.

```json
{
  "items": [
    {"id": 1, "owner_id": 2, "name": "Toyota Yaris", "price_per_day": 45000,
     "active": true, "owner_email": "seller@example.com"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

**Errors:** `400` with `invalid limit`, `invalid offset`, `invalid owner_id`
or `invalid active`.

---

## `PATCH /admin/cars/{id}`

Edits any car, regardless of its owner. It accepts the same fields as
`PATCH /seller/cars/{id}` (`name`, `photo_url`, `price_per_day`, `active`), at
least one.

The difference with the seller route: **the administrator can deactivate a car
that has future reservations**. The seller cannot (it would get `409`), because
deactivating itself would break its open contracts; the administrator can,
because taking down a problematic listing is exactly the use case.

**Answers** `200 {"status":"ok"}`.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid car id` | `{id}` is not an integer |
| `400` | `no fields to update` | Body empty of known fields |
| `400` | *(validation message)* | Invalid name, price or photo |
| `404` | `car not found` | The car does not exist |

---

## `DELETE /admin/cars/{id}`

Deletes any car without reservation history, regardless of its owner.

A car that has reservations is **not** deleted: it answers `409`. The reservation
rows reference the car, so a delete would break the history. To take it out of
the public view you have to deactivate it (`PATCH` with `active: false`), which
is what the seller route does too.

**Answers** `200 {"status":"ok"}`.

**Errors:** `400 invalid car id`, `404 car not found`, `409 car has reservations,
cannot delete`.

---

## `GET /admin/reservations`

Lists every reservation, with `buyer_email` and `car_name` resolved so there is
no need to chain calls.

**Query:** `status` (`pending`, `accepted`, `confirmed`, `rejected`,
`cancelled`), `user_id`, `car_id`, `limit`, `offset`.

```json
{
  "items": [
    {"id": 1, "user_id": 3, "car_id": 1, "start_date": "2026-10-05",
     "end_date": "2026-10-07", "status": "confirmed",
     "buyer_email": "buyer@example.com", "car_name": "Toyota Yaris"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

## `PATCH /admin/reservations/{id}/confirm`

The second half of the two-step confirmation: the administrator validates the payment of a reservation the owner already accepted, approving the payment and marking the reservation as confirmed, atomically. A reservation only becomes `confirmed` through this endpoint. Both the confirmation and the correction request below are written to the audit log (`reservation.confirm`, `reservation.request_correction`). No body.

**Answers** `200`:

```json
{
  "id": 1, "user_id": 3, "car_id": 1,
  "start_date": "2026-10-05", "end_date": "2026-10-07", "status": "confirmed",
  "payment": {"id": 1, "reservation_id": 1, "method": "pos", "status": "approved"}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist |
| `409` | `reservation is not accepted` | The owner has not accepted it yet, or it already moved on |
| `409` | `no payment recorded for this reservation` | The buyer has not attached the proof yet |
| `409` | `payment is not pending` | The payment already moved |

---

## `PATCH /admin/reservations/{id}/request-correction`

Sends the payment back to the buyer: the payment moves to `rejected` but the reservation stays `accepted`, so the buyer attaches a corrected proof instead of starting over. No body.

**Answers** `200`: the reservation view with `status: accepted` and its payment `rejected`.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist |
| `409` | `reservation is not accepted` | The owner has not accepted it yet, or it already moved on |
| `409` | `payment is not pending` | There is no pending payment to send back |

---

## `GET /admin/notifications`

Every notification on the platform, newest first, so administrators can audit
what each account was told.

**Query:** `user_id`, `kind` (one of `reservation.requested`,
`reservation.accepted`, `reservation.rejected`, `reservation.confirmed`,
`reservation.correction_requested`), `unread` (`true`/`false`), `limit`,
`offset`.

**Answers** `200`: `{"items": [...], "total": N, "limit": 50, "offset": 0}`,
where each item is a [notification](notifications.md).

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid kind` | Unknown kind |
| `400` | `invalid unread` | `unread` is neither `true` nor `false` |

---

## `GET /admin/payments`

Lists every payment, with the reservation and buyer context.

**Query:** `status` (`pending`, `approved`, `rejected`), `reservation_id`,
`limit`, `offset`.

```json
{
  "items": [
    {"id": 1, "reservation_id": 1, "method": "pos", "status": "approved",
     "proof_url": "uploads/x.png", "user_id": 3, "car_id": 1,
     "buyer_email": "buyer@example.com"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

## `GET /admin/stats`

Platform metrics, in a single call. Read only, no pagination.

```json
{
  "users": {"total": 12, "sellers": 4, "admins": 1, "suspended": 1},
  "cars": {"active": 9},
  "reservations": {"pending": 3, "accepted": 1, "confirmed": 5, "rejected": 0, "cancelled": 2},
  "payments": {"pending": 1, "approved": 4, "rejected": 1, "approved_total": 540000}
}
```

A reservation has no `paid` state: the CHECK on the table admits `pending`,
`accepted`, `confirmed`, `rejected` and `cancelled`. That a reservation is paid
is inferred from its payment being `approved`, and that is what
`payments.approved` counts.

`approved_total` adds up `price_per_day` × days of every reservation with an
approved payment. Beware: reservations store dates, not price, so the figure
uses the **current** price of the car, not the one agreed on when booking. It is
an approximation to size the platform, not a revenue ledger.

---

## `GET /admin/audit`

History of human decisions on the platform — admin operations plus the
seller/buyer decisions that move a reservation. Each row says who did what, to
whom, and when. The indexes are on `actor_id` and on `action`, and the order
is from most recent to oldest.

**Query:** `actor_id`, `action`, `limit`, `offset`.

```json
{
  "items": [
    {"id": 12, "actor_id": 1, "actor_email": "admin@example.com",
     "action": "user.suspend", "target_type": "users", "target_id": 3,
     "created_at": "2026-10-03 15:12:04"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

### Audited actions

| `action` | Source | What it records |
|----------|--------|-----------------|
| `user.suspend` | `PATCH /admin/users/{id}` | Account suspended |
| `user.unsuspend` | `PATCH /admin/users/{id}` | Account reactivated |
| `user.role_grant_seller` | `PATCH /admin/users/{id}/roles` | Seller granted |
| `user.role_revoke_seller` | `PATCH /admin/users/{id}/roles` | Seller revoked |
| `car.update` | `PATCH /admin/cars/{id}` | Car edited or deactivated |
| `car.delete` | `DELETE /admin/cars/{id}` | Car deleted |
| `reservation.accept` | `PATCH /seller/reservations/{id}/accept` | Owner accepted (actor: seller) |
| `reservation.reject` | `PATCH /seller/reservations/{id}/reject` | Owner rejected (actor: seller) |
| `reservation.cancel` | `PATCH /reservations/{id}/cancel` | Buyer cancelled (actor: buyer) |
| `reservation.confirm` | `PATCH /admin/reservations/{id}/confirm` | Payment approved, reservation confirmed |
| `reservation.request_correction` | `PATCH /admin/reservations/{id}/request-correction` | Payment sent back for correction |

The rule is decisions, not mechanics: booking, recording a payment and
reading (`GET`) change no decision and are not audited — the first two stay
visible in their listings. See [`notifications.md`](notifications.md) for the
full event → notification → audit table.

Every write happens **inside the same transaction** as the change that caused
it: either both land, or neither does. That is why an idempotent operation that
changes nothing does not add a row to the history either.

Administrative reads (`GET`) are not audited: they fill the log with noise
without recording any decision.

The log outlives the account that wrote it: `actor_id` has no delete cascade. If
a user were ever deleted, the `actor_id` would point at a non-existent account,
which is why the query resolves it with a `LEFT JOIN` (`actor_email` can come
back empty).