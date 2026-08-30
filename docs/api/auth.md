# Auth

## `GET /health`

Healthcheck público. Reporta estado de la base, la versión del build (se
inyecta al compilar con `-ldflags "-X main.version=<version>"`; en dev es
`"dev"`) y responde `503` si la base no responde.

**Respuesta** — `200 OK`:

```json
{"status":"ok","version":"dev"}
```

**Respuesta** — `503 Service Unavailable` (base caída):

```json
{"status":"degraded","version":"dev"}
```

**Errores:** ninguno.

---

## `POST /auth/register`

Crea una cuenta `buyer`. Público (rate-limited).

**Body:**

| Campo | Tipo | Requerido | Reglas |
|-------|------|-----------|--------|
| `email` | string | sí | formato email; se normaliza a minúsculas |
| `password` | string | sí | mínimo 6 caracteres |

```json
{"email":"demo@example.com","password":"secret123"}
```

**Respuesta** — `201 Created`:

```json
{"id":3,"email":"demo@example.com"}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid email` | El email no pasa la validación de formato |
| `400` | `password must be at least 6 characters` | Password muy corto |
| `409` | `email already registered` | El email ya existe |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

---

## `POST /auth/register/seller`

Crea una cuenta `seller` (vendedor) que puede publicar y gestionar **sus**
autos. Mismo body y validaciones que `/auth/register`. Público (rate-limited).

**Respuesta** — `201 Created`:

```json
{"id":4,"email":"vendedor@example.com"}
```

**Errores:** idénticos a `/auth/register`.

---

## `POST /auth/login`

Valida credenciales y devuelve un token JWT (válido 24 h). Público
(rate-limited).

**Body:**

```json
{"email":"vendedor@example.com","password":"secret123"}
```

**Respuesta** — `200 OK`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `invalid credentials` | El email no existe o el password no coincide (mismo mensaje en ambos casos) |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

---

## `GET /auth/me`

Devuelve el perfil del usuario autenticado. Útil para que el frontend muestre
el email y el rol sin decodificar el JWT. Requiere login.

Usa el token como `Authorization: Bearer <token>`.

**Respuesta** — `200 OK`:

```json
{"id":3,"email":"demo@example.com","role":"buyer"}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `missing bearer token` | Sin cabecera `Authorization` o sin prefijo `Bearer` |
| `401` | `invalid or expired token` | Token inválido o vencido |
| `401` | `user not found` | El `uid` del token no existe en la base |

Nota: al vivir bajo `/auth/`, hereda el rate limit de ese prefijo; cacheá la
respuesta en el cliente en vez de pollearla.

---

**Nota:** `/auth/*` está rate-limitado por IP (30 req/60 s → `429`).