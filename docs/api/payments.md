# Payments

## `POST /reservations/{id}/payment`

Records the payment for your reservation. Requires login.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `method` | text | yes | `pos` or `cash` |
| `proof_url` | text | no | when present, an `http(s)` URL or a `/uploads/...` path of up to 2048 (see [uploads](uploads.md)) |

```json
{"method":"pos","proof_url":"https://example.com/receipt.pdf"}
```

Rules: the reservation must exist and be yours, must not be `cancelled` and must not already have a payment (one per reservation).

**Answers** `201` (born `pending`):

```json
{
  "id":1,
  "reservation_id":1,
  "method":"pos",
  "status":"pending",
  "proof_url":"https://example.com/receipt.pdf"
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `400` | `method must be pos or cash` | Unknown method |
| `400` | `proof_url is too long` | More than 2048 |
| `400` | `invalid proof_url` | Malformed URL, neither `http(s)` nor a valid `/uploads/...` path |
| `404` | `reservation not found` | It does not exist or it is not yours |
| `409` | `reservation is not pending` | It is cancelled or confirmed |
| `409` | `payment already recorded` | It already has a payment |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |

---

## `PATCH /seller/reservations/{id}/confirm`

The seller approves the payment and confirms the reservation, both in a single transaction. Requires `seller` membership and the car to be yours. No body.

It moves `payments.status` → `approved` and `reservations.status` → `confirmed`.

**Answers** `200`:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"confirmed",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"approved","proof_url":"https://..."}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or the car is not yours |
| `409` | `reservation is not pending` | Already confirmed or cancelled |
| `409` | `no payment recorded for this reservation` | There is no payment to approve |

---

## `PATCH /seller/reservations/{id}/reject`

Rejects the recorded payment and **cancels the reservation** in the same transaction, releasing the dates. It is the way out for a fraudulent or never-arrived transfer: without this endpoint a paid reservation could only move to `confirmed`. Requires `seller` membership and the car to be yours. No body.

It moves `payments.status` → `rejected` and `reservations.status` → `cancelled`. The payment row is kept (audit trail), and since there can only be one payment per reservation, the buyer must create a new reservation if they still want to book.

**Answers** `200`:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"cancelled",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"rejected","proof_url":"https://..."}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or the car is not yours |
| `409` | `reservation is not pending` | Already confirmed or cancelled |
| `409` | `no payment recorded for this reservation` | There is no payment to reject |
| `409` | `payment is not pending` | The payment is no longer `pending` |