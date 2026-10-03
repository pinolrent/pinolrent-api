# Cars

## `GET /cars`

Lists the **active** cars of every seller. You can filter by seller and by dates (excludes the ones already booked in that range). Requires no login.

**Filters** (all optional, they can be combined):

| Parameter | Format | Rules |
|-----------|--------|-------|
| `start_date` | `YYYY-MM-DD` | must come with `end_date` |
| `end_date` | `YYYY-MM-DD` | `>= start_date` |
| `owner_id` | number | only the cars of that seller |
| `limit` | number | `1..200`, default `50` |
| `offset` | number | `>= 0`, default `0` |

It only shows cars with `active=1` and without a `pending`/`confirmed` reservation that collides with `[start_date, end_date]`. `cancelled` ones do not block.

**Answers** `200`:

```json
[
  {"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true},
  {"id":2,"owner_id":7,"name":"Fiat Cronos","price_per_day":38000,"active":true}
]
```

No results → `[]`.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `start_date and end_date must be provided together` | You sent only one of them |
| `400` | `invalid start_date, expected YYYY-MM-DD` | Bad format |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Bad format |
| `400` | `end_date must be on or after start_date` | Range reversed |
| `400` | `invalid owner_id` | Not a valid number |
| `400` | `invalid limit` / `invalid offset` | Bad pagination (`offset` max 10000) |

---

## `GET /cars/{id}`

Detail of an **active** car. Requires no login.

**Answers** `200`:

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true}
```

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid car id` | `{id}` is not a number |
| `404` | `car not found` | It does not exist or it is inactive |

---

## `GET /cars/{id}/contact`

Returns the seller's WhatsApp link to coordinate. **Requires login** — it is the only place a phone number is exposed, on purpose: the public catalog cannot be harvested.

```
Authorization: Bearer <token>
```

**Answers** `200`:

```json
{"whatsapp_url":"https://wa.me/56912345678?text=Hi%2C%20I%20saw%20your%20Toyota%20Yaris%20on%20PinolRent"}
```

The message comes prefilled with the car name, and the number uses the international format without `+`, as `wa.me` requires.

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid car id` | `{id}` is not a number |
| `401` | see [00-general](00-general.md) | No token |
| `404` | `car not found` | It does not exist or it is inactive |
| `409` | `seller has no contact phone` | The seller has no phone on file |

> It counts against the strict 30/min quota (the same one as `/auth/*`), because it exposes personal data.

---

## `GET /seller/cars`

Your cars as a seller, newest first. Requires `seller` membership. Accepts `limit`/`offset`.

```json
[{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}]
```

No cars → `[]`.

---

## `POST /seller/cars`

Adds one of your cars. Requires `seller` membership.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `name` | text | yes | not empty, up to 200 characters |
| `photo_url` | text | no | when present, an `http(s)` URL or a `/uploads/...` path of up to 2048 (see [uploads](uploads.md)) |
| `price_per_day` | number | no | `0..100_000_000` cents |

```json
{"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000}
```

**Answers** `201` (born `active: true` with your `owner_id`):

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000,"active":true}
```

| Status | Message | When |
|--------|---------|------|
| `400` | `name is required` | Empty or missing |
| `400` | `name is too long (max 200 characters)` | More than 200 |
| `400` | `price_per_day must be >= 0` | Negative |
| `400` | `price_per_day must be <= 100000000` | Over the cap |
| `400` | `photo_url is too long` | More than 2048 |
| `400` | `invalid photo_url` | Malformed URL, neither `http(s)` nor a valid `/uploads/...` path |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |
| `401` / `403` | see [00-general](00-general.md) | No token or no permission |

---

## `PATCH /seller/cars/{id}`

Edits one of your cars. Requires `seller` membership. Accepts **any combination** of these fields (at least one):

| Field | Type | Rules |
|-------|------|-------|
| `name` | text | not empty, up to 200 characters |
| `photo_url` | text | `http(s)` URL or `/uploads/...` path of up to 2048; empty removes the photo |
| `price_per_day` | number | `0..100_000_000` cents. Applies to **future reservations**: the existing ones do not change |
| `active` | boolean | turns the car on or off (rules below) |

```json
{"name":"Toyota Yaris LX","price_per_day":46000}
```

**Answers** `200` with the updated car:

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris LX","photo_url":"https://example.com/yaris.jpg","price_per_day":46000,"active":true}
```

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid car id` | `{id}` is not a number |
| `400` | `no fields to update` | Body without any field |
| `400` | `name is required` / `name is too long` / `price_per_day ...` / `invalid photo_url` | Creation rules, same messages |
| `409` | `car has future reservations, cannot deactivate` | You tried to turn it off with future reservations |
| `404` | `car not found` | It does not exist or it is not yours |
| `401` / `403` | see [00-general](00-general.md) | No token or no permission |

Editing the name, price or photo is **not** blocked by existing reservations; the guard only applies to the transition to `active:false`.

---

## `DELETE /seller/cars/{id}`

Deletes a car that **never had reservations** (in any state). Requires `seller` membership. With reservation history it answers `409`, because every reservation references its car: in that case use `PATCH` with `active:false` to take it out of the catalog. The orphan sweeper cleans up the car photo afterwards.

**Answers** `200`:

```json
{"status":"ok"}
```

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid car id` | `{id}` is not a number |
| `404` | `car not found` | It does not exist or it is not yours |
| `409` | `car has reservations, cannot delete` | It has reservations (in any state) |
| `401` / `403` | see [00-general](00-general.md) | No token or no permission |

---

> Turning a car off does not delete its old reservations, it only stops showing up in `GET /cars` and stops accepting new reservations (`409 car is not active`). If it has future reservations (`pending`/`confirmed`) it cannot be deactivated (`409`). If it never had reservations, `DELETE` removes it for good.