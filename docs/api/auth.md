# Auth

## `GET /health`

Dice si el server y la base están bien. No necesita login.

- Responde `200 {"status":"ok","version":"..."}` si todo ok.
- Responde `503 {"status":"degraded","version":"..."}` si la base no responde.
- `version` viene de compilar con `make build` (en dev es `"dev"`).

---

## `POST /auth/register`

Crea cuenta de **comprador**. No necesita login.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `email` | texto | sí | formato email (TLD de 2+ letras, sin `..` ni punto/guion en bordes), hasta 254 caracteres, se guarda en minúsculas |
| `password` | texto | sí | 8 a 72 caracteres |
| `phone` | texto | no | teléfono de contacto; se normaliza a E.164 (ver abajo) |

```json
{"email":"demo@example.com","password":"secret123","phone":"+56912345678"}
```

**Responde** `201`:

```json
{"email":"demo@example.com"}
```

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid email` | Email mal formado |
| `400` | `email is too long` | Más de 254 |
| `400` | `password must be 8-72 characters` | Fuera de rango |
| `400` | `invalid phone` | Teléfono mal formado |
| `400` | `invalid JSON body` | JSON roto o campos que no existen |
| `413` | `request body too large` | Más de 1 MB |

> Si el email ya existe, responde el mismo `201 {"email":"..."}` para no revelar qué emails están registrados.

**Formatos de `phone`:** se acepta formato chileno local (`912345678`, `9 1234 5678`), con código de país sin `+` (`56912345678`) o internacional completo (`+56912345678`, `+14155552671`). Se guardan siempre en E.164 (`+56912345678`), que es lo que necesitan los links de WhatsApp.

---

## `POST /auth/register/seller`

Igual que el anterior pero crea cuenta de **vendedor**. Mismo body y mismas reglas, con una diferencia: **`phone` es obligatorio**, porque es el número que usan los compradores para contactarlo.

```json
{"email":"vendedor@example.com","password":"secret123","phone":"+56912345678"}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `phone is required for sellers` | Falta el teléfono |
| `400` | `invalid phone` | Teléfono mal formado |

---

## `POST /auth/login`

Verifica email y password y devuelve un token. No necesita login.

```json
{"email":"vendedor@example.com","password":"secret123"}
```

**Responde** `200`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...","refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `invalid credentials` | Email no existe o password mal (mismo mensaje siempre) |
| `400` | `invalid JSON body` | JSON roto o campos desconocidos |
| `413` | `request body too large` | Más de 1 MB |

> El tiempo de respuesta es el mismo exista o no el email (hace un `bcrypt` dummy si no existe), así no se puede adivinar qué emails están registrados.

---

## `POST /auth/refresh`

Cambia un refresh token de un solo uso por un par nuevo (`token` + `refresh_token`). El presentado queda revocado: reusarlo devuelve `401`.

```json
{"refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

**Responde** `200`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...","refresh_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `invalid or expired token` | Refresh trucho, vencido o ya usado |
| `400` | `refresh_token is required` | Falta el campo |
| `400` | `invalid JSON body` | JSON roto o campos desconocidos |

---

## `POST /auth/logout`

Invalida el token que mandaste en el header. Ese token deja de funcionar (responde `401` de ahí en más). Otros tokens del mismo usuario siguen valiendo — es por token, no por usuario.

Necesita login. Sin body.

**Responde** `200`:

```json
{"status":"ok"}
```

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `missing bearer token` | Sin header |
| `401` | `invalid or expired token` | Token trucho, vencido o ya revocado |
| `400` | `token cannot be revoked` | Token sin `jti` (no pasa con tokens de esta versión) |
| `500` | `server error` | Error de base al guardar |

Las filas se borran solas cada 10 minutos cuando el token ya habría vencido.

---

## `GET /auth/me`

Devuelve tu perfil. Necesita login.

```
Authorization: Bearer <token>
```

**Responde** `200`:

```json
{"id":3,"email":"demo@example.com","role":"buyer","phone":"+56912345678"}
```

`phone` viene vacío (`""`) si no cargaste uno; los vendedores siempre lo tienen.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `missing bearer token` | Sin header |
| `401` | `invalid or expired token` | Token trucho o vencido |
| `401` | `user not found` | El usuario del token ya no existe |

> Está bajo `/auth/` así que cuenta para el límite de 30/min. Mejor cachear la respuesta en el cliente.

---

## `PATCH /auth/me`

Actualiza tu teléfono. Necesita login. Es lo único editable: el email identifica la cuenta y el rol se define al registrarse.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `phone` | texto | sí | mismas reglas que en el registro (se normaliza a E.164) |

```json
{"phone":"9 8765 4321"}
```

**Responde** `200` con el perfil actualizado, igual que `GET /auth/me`.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid phone` | Teléfono mal formado |
| `400` | `phone is required for sellers` | Un vendedor intentó dejarlo vacío |
| `400` | `invalid JSON body` | Mandaste `email` o `role` (no son editables) |
| `401` | ver arriba | Sin token o token inválido |

---

## Cómo es el token (JWT)

Lo que devuelve `POST /auth/login` es un JWT firmado con **HS256**. El access dura **15 min** y el refresh **7 días** (un solo uso, con rotación):

| Claim | Qué es |
|-------|--------|
| `uid` | tu id |
| `role` | `buyer` o `seller` |
| `sub` | tu id como texto |
| `iss` | `pinolrent-api` |
| `aud` | `pinolrent-api` (access) o `pinolrent-api-refresh` (refresh) |
| `jti` | id único del token (32 hex chars) |
| `iat` | cuándo se emitió |
| `exp` | cuándo vence |

El `jti` es lo que permite invalidar un token con `/auth/logout`. El server lo guarda en `revoked_tokens` y lo revisa en cada request protegida.

El server rechaza tokens con:

- Algoritmo que no sea `HS256` (incluido `none`).
- Falta de `exp`, `iss` o `aud`.
- Firma inválida, vencido o `jti` revocado.

---

> `/auth/*` limitado a 30 por minuto por IP → `429`.
