# Auth

## `GET /health`

Tells whether the server and the database are fine. Requires no login.

- Answers `200 {"status":"ok","version":"..."}` if everything is ok.
- Answers `503 {"status":"degraded","version":"..."}` if the database does not answer.
- `version` comes from building with `make build` (in dev it is `"dev"`).

---

## `POST /auth/register`

Creates an account. One account per email: without `phone` it is only a buyer (`["buyer"]`), with a valid `phone` it is a buyer and a seller from the start (`["buyer","seller"]`). A buyer account can upgrade to seller later with `POST /auth/become-seller`. Requires no login.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `email` | text | yes | email format (TLD of 2+ letters, no `..` nor leading/trailing dot or hyphen), up to 254 characters, stored lowercase |
| `password` | text | yes | 8 to 72 characters |
| `phone` | text | no | contact phone; when present it is normalized to E.164 (see below) and the account is also born a seller |

```json
{"email":"demo@example.com","password":"secret123","phone":"+50581234567"}
```

**Answers** `201`:

```json
{"email":"demo@example.com"}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid email` | Malformed email |
| `400` | `email is too long` | More than 254 |
| `400` | `password must be 8-72 characters` | Out of range |
| `400` | `invalid phone` | Malformed phone |
| `400` | `invalid JSON body` | Broken JSON or fields that do not exist |
| `413` | `request body too large` | More than 1 MB |

> If the email already exists, it answers the same `201 {"email":"..."}` to avoid revealing which emails are registered.

**`phone` formats:** Local Nicaraguan numbers are accepted (`81234567`, `8123-4567`), with a country code without `+` (`50581234567`) or fully international (`+50581234567`, `+14155552671`). They are always stored in E.164 (`+50581234567`), which is what the WhatsApp links need. The default country comes from `PHONE_COUNTRY_PREFIX` / `PHONE_NATIONAL_LEN` (see [configuration](../configuration.md)).

---

## `POST /auth/become-seller`

Turns your buyer account into a buyer + seller. Requires login. It is the upgrade path `["buyer"]` → `["buyer","seller"]`; it is idempotent (if you are already a seller, it returns your current profile).

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `phone` | text | yes | E.164 contact phone (same rules as on registration) |

```json
{"phone":"+50581234567"}
```

**Answers** `200` with your updated profile, same as `GET /auth/me`.

| Status | Message | When |
|--------|---------|------|
| `400` | `phone is required for sellers` | The phone is missing or malformed |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `401` | see below | No token or invalid token |

---

## `POST /auth/login`

Checks the email and password and returns a token. Requires no login.

```json
{"email":"seller@example.com","password":"secret123"}
```

**Answers** `200`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...","refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `401` | `invalid credentials` | The email does not exist or the password is wrong (always the same message) |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |

> The response time is the same whether the email exists or not (it runs a dummy `bcrypt` when it does not), so you cannot guess which emails are registered.

---

## `POST /auth/refresh`

Exchanges a single-use refresh token for a new pair (`token` + `refresh_token`). The submitted one is revoked: reusing it returns `401` **and also invalidates every other token of the user** — a reuse is a signal of token theft and the server cannot tell the thief from the victim, so it closes both sessions (you have to log in again).

```json
{"refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

**Answers** `200`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...","refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

| Status | Message | When |
|--------|---------|------|
| `401` | `invalid or expired token` | Forged, expired or already used refresh token |
| `400` | `refresh_token is required` | The field is missing |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |

---

## `POST /auth/logout`

Invalidates the token you sent in the header. That token stops working (it answers `401` from then on). Other tokens of the same user keep working — it is per token, not per user.

Requires login. No body.

**Answers** `200`:

```json
{"status":"ok"}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `401` | `missing bearer token` | No header |
| `401` | `invalid or expired token` | Forged, expired or already revoked token |
| `500` | `server error` | Database error while saving |

The rows delete themselves every 10 minutes once the token would have expired anyway.

---

## `GET /auth/me`

Returns your profile. Requires login.

```
Authorization: Bearer <token>
```

**Answers** `200`:

```json
{"id":3,"email":"demo@example.com","roles":["buyer"],"phone":"+50581234567"}
```

`phone` comes back empty (`""`) if you did not set one; sellers always have it. `roles` is `["buyer"]` or `["buyer","seller"]`, or `["admin","buyer"]` if the account is on the `ADMIN_EMAILS` allow-list. See [`admin.md`](admin.md).

| Status | Message | When |
|--------|---------|------|
| `401` | `missing bearer token` | No header |
| `401` | `invalid or expired token` | Forged or expired token |
| `401` | `user not found` | The user behind the token no longer exists |

> It lives under `/auth/`, so it counts against the 30/min limit. Better to cache the response on the client.

---

## `PATCH /auth/me`

Updates your phone. Requires login. It is the only editable field: the email identifies the account and the roles are granted on registration (with `phone`) or through `POST /auth/become-seller`. Setting `phone` here never makes you a seller.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `phone` | text | yes | same rules as on registration (normalized to E.164) |

```json
{"phone":"9 8765 4321"}
```

**Answers** `200` with the updated profile, same as `GET /auth/me`.

| Status | Message | When |
|--------|---------|------|
| `400` | `invalid phone` | Malformed phone |
| `400` | `phone is required for sellers` | A seller tried to leave it empty |
| `400` | `invalid JSON body` | You sent `email` or `roles` (they are not editable) |
| `401` | see above | No token or invalid token |

---

## `PATCH /auth/password`

Changes your password. Requires login. **Revokes all your sessions**, including the one making this request: every token issued before this moment stops being valid, so after the change you have to log in again with the new password.

**Body:**

| Field | Type | Required? | Rules |
|-------|------|-----------|-------|
| `current_password` | text | yes | your current password |
| `new_password` | text | yes | 8 to 72 characters |

```json
{"current_password":"secret123","new_password":"newSecret456"}
```

**Answers** `200`:

```json
{"status":"ok"}
```

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `401` | `invalid credentials` | The current password does not match |
| `400` | `password must be 8-72 characters` | The new one is out of range |
| `400` | `invalid JSON body` | Broken JSON or unknown fields |
| `413` | `request body too large` | More than 1 MB |
| `401` | see the table above | No token or revoked token |

---

## What the token looks like (JWT)

What `POST /auth/login` returns is a JWT signed with **HS256**. The access token lasts **15 min** and the refresh token **7 days** (single use, with rotation):

| Claim | What it is |
|-------|------------|
| `uid` | your id |
| `roles` | `["buyer"]` or `["buyer","seller"]` |
| `sub` | your id as text |
| `iss` | `pinolrent-api` |
| `aud` | `pinolrent-api` (access) or `pinolrent-api-refresh` (refresh) |
| `jti` | unique token id (32 hex chars) |
| `iat` | when it was issued |
| `exp` | when it expires |

The `jti` is what makes it possible to invalidate a token with `/auth/logout`. The server stores it in `revoked_tokens` and checks it on every protected request.

The server rejects tokens with:

- An algorithm other than `HS256` (including `none`).
- A missing `exp`, `iss`, `aud` or `roles`.
- An invalid signature, expired, or a revoked `jti`.

Tokens issued before the migration to `roles` (with the singular `role` claim) are rejected: you have to log in again.

---

> `/auth/*` is limited to 30 per minute per IP → `429`.