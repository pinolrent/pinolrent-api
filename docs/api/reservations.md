# Reservations

All of them need `Authorization: Bearer <token>`.

## `POST /reservations`

Books a car. Requires login.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `car_id` | number | yes | must exist and be active |
| `start_date` | `YYYY-MM-DD` | yes | cannot be before today (UTC) |
| `end_date` | `YYYY-MM-DD` | yes | `>= start_date`, at most a 30-day range |

```json
{"car_id":1,"start_date":"2026-10-01","end_date":"2026-10-05"}
```

If two people try to book the same car for the same dates, only one goes through (it uses a transaction).

**Answers** `201` (with the car included, no payment yet):

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid start_date, expected YYYY-MM-DD` | Bad format |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Bad format |
| `400` | `end_date must be on or after start_date` | Range reversed |
| `400` | `start_date cannot be in the past` | Date in the past |
| `400` | `reservation cannot be longer than 30 days` | More than 30 days |
| `400` | `car_id is required` | Missing or 0 |
| `404` | `car not found` | Does not exist |
| `409` | `car is not active` | It exists but is deactivated |
| `409` | `car already reserved for the requested dates` | There is already a reservation for those dates |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |
| `401` | see [00-general](00-general.md) | No token |

---

## `GET /reservations`

Your reservations, newest first. Accepts `limit`/`offset`.

```json
[
  {
    "id":1,"user_id":3,"car_id":1,
    "start_date":"2026-10-01","end_date":"2026-10-05",
    "status":"confirmed",
    "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
    "payment":{"id":1,"reservation_id":1,"method":"pos","status":"approved","proof_url":"https://..."}
  }
]
```

No reservations → `[]`.

---

## `GET /reservations/{id}`

Detail of a reservation. Visible to the buyer who made it or to the seller who owns the car.

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or it is not yours |

---

## `PATCH /reservations/{id}/cancel`

Cancels your reservation. Requires login.

Only if: it is yours, it is `pending` and it has **no payment** (if you already paid, talk to the seller).

**Answers** `200` with the reservation already `cancelled`.

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or it is not yours |
| `409` | `reservation is not pending` | Already confirmed or cancelled |
| `409` | `payment already recorded, cannot cancel` | It already has a payment |

Cancelling makes those dates available again.

---

## `GET /seller/reservations`

Reservations for your cars as a seller, newest first. Requires `seller` membership. Accepts `limit`/`offset`.

An array of reservations. No reservations → `[]`.