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

Rules: the reservation must exist and be yours, must still be `pending` or `accepted`, and must not already have a payment — except after a correction: when the reservation is `accepted` and its payment was `rejected` by the administrator, posting again replaces the proof and returns the payment to `pending` (answers `200` instead of `201`).

**Answers** `201` (born `pending`), `200` (corrected proof, back to `pending`):

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
| `409` | `reservation is not pending` | It is confirmed, rejected or cancelled (`accepted` is allowed: it is the correction path) |
| `409` | `payment already recorded` | It already has a payment |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |

---

## `PATCH /seller/reservations/{id}/accept`

The owner accepts the buyer's request: the first half of the two-step confirmation. The reservation moves `pending` → `accepted`; it still needs the administrator to validate the payment (see `PATCH /admin/reservations/{id}/confirm` in [admin](admin.md)). No payment is required at this point: the owner decides on the request, the administrator on the money. Requires `seller` membership and the car to be yours. No body.

**Answers** `200` (the payment, if any, is untouched):

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"accepted",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"pending","proof_url":"https://..."}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or the car is not yours |
| `409` | `reservation is not pending` | Already accepted, confirmed, rejected or cancelled |

---

## `PATCH /seller/reservations/{id}/reject`

The owner turns the buyer's request down, releasing the dates. It moves `reservations.status` → `rejected`; when a payment is recorded and still `pending` it moves to `rejected` alongside as an audit trail. No payment is required: the owner decides on the request itself. Requires `seller` membership and the car to be yours. No body.

**Answers** `200`:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"rejected",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"rejected","proof_url":"https://..."}
}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid reservation id` | `{id}` is not a number |
| `404` | `reservation not found` | It does not exist or the car is not yours |
| `409` | `reservation is not pending` | Already accepted, confirmed, rejected or cancelled |