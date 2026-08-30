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

Crea una cuenta `client`. Público (rate-limited).

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

## `POST /auth/login`

Valida credenciales y devuelve un token JWT (válido 24 h). Público
(rate-limited).

**Body:**

```json
{"email":"admin@pinolrent.com","password":"admin123"}
```

**Respuesta** — `200 OK`:

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

Usa el token como `Authorization: Bearer <token>`.

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `invalid credentials` | El email no existe o el password no coincide (mismo mensaje en ambos casos) |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

---

**Nota:** `/auth/*` está rate-limitado por IP (30 req/60 s → `429`).