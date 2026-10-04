# Notifications

In-app notes about the reservation flow, written in the same transaction as
the event they announce. There is no external delivery (no email or push):
the frontend polls. Kinds: `reservation.requested` (new booking, to the owner
and the administrators), `reservation.accepted` (to the buyer),
`reservation.rejected` (to the buyer and the administrators),
`reservation.confirmed` (to the buyer, the owner and the administrators) and
`reservation.correction_requested` (to the buyer).

## `GET /notifications`

Your notifications, newest first. Requires login.

**Query:** `unread` (`true` for unread only), `limit`, `offset`.

**Answers** `200`:

```json
[
  {"id":1,"user_id":3,"kind":"reservation.accepted","reservation_id":1,
   "read":false,"created_at":"2026-10-04 00:00:00"}
]
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid unread` | `unread` is neither `true` nor `false` |

---

## `PATCH /notifications/{id}/read`

Marks one of your notifications as read. Idempotent: reading twice still
answers `200`. Someone else's notification answers `404`, as if it did not
exist. No body.

**Answers** `200`: the notification with `"read":true`.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid notification id` | `{id}` is not a number |
| `404` | `notification not found` | It does not exist or it is not yours |
