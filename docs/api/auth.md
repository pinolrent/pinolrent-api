# Auth

## `GET /health`

Healthcheck público.

**Respuesta** — `200 OK`:

```json
{"status":"ok"}
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

---

## `GET /auth/me`

Devuelve el perfil del usuario autenticado. Útil para que el frontend muestre
el email y el rol sin decodificar el JWT. Requiere login.

**Respuesta** — `200 OK`:

```json
{"id":3,"email":"demo@example.com","role":"buyer"}
```

**Errores:** `401` igual que las demás rutas protegidas (token ausente,
inválido o vencido). Nota: al vivir bajo `/auth/`, hereda el rate limit de ese
prefijo; cacheá la respuesta en el cliente en vez de pollearla.

Usa el token como `Authorization: Bearer <token>`.

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `invalid credentials` | El email no existe o el password no coincide (mismo mensaje en ambos casos) |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

---

**Nota:** `/auth/*` está rate-limitado por IP (30 req/60 s → `429`).