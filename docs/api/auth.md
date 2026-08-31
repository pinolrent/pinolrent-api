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
| `email` | string | sí | formato email; ≤ 254 caracteres; se normaliza a minúsculas |
| `password` | string | sí | 8 a 72 caracteres (bcrypt trunca silenciosamente a 72 bytes) |

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
| `400` | `email is too long` | Email > 254 caracteres |
| `400` | `password must be 8-72 characters` | Password fuera de rango |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

> Si el email ya existe, el endpoint responde `201` con `{"id":0,"email":"..."}`
> en vez de `409`, para no permitir enumerar cuentas registradas. La
> diferenciación se hace vía `/auth/login` con un mensaje uniforme y un
> bcrypt dummy en el camino de "no existe" para igualar el tiempo de
> respuesta.

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

> El tiempo de respuesta es **constante** independientemente de si el email
> existe: en el camino de "no existe" se ejecuta un `bcrypt.Compare` contra
> un hash dummy, así un atacante no puede enumerar emails midiendo timing.

---

## `POST /auth/logout`

Revoca el token bearer usado en la request. El mismo token (o cualquier
otro con el mismo `jti`) es rechazado con `401` por el middleware de auth
desde ese momento. La revocación es **por token, no por usuario**: otros
tokens del mismo usuario siguen siendo válidos hasta su `exp` natural.

Requiere login. No tiene body.

**Respuesta** — `200 OK`:

```json
{"status":"ok"}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `missing bearer token` | Sin cabecera `Authorization` o sin prefijo `Bearer` |
| `401` | `invalid or expired token` | Token inválido, vencido, o ya revocado |
| `400` | `token cannot be revoked` | El token no lleva `jti` (imposible en tokens emitidos por esta versión) |
| `500` | `server error` | Error de base de datos al insertar la revocación |

Las filas de `revoked_tokens` se podan automáticamente cada 10 minutos (GC
interno) una vez que el `exp` del token ya pasó, así la tabla se mantiene
pequeña sin intervención manual.

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

## Contrato del JWT

El token que devuelve `POST /auth/login` es un JWT firmado con **HS256** y
válido por **24 h**. Claims incluidos:

| Claim | Tipo | Valor |
|-------|------|-------|
| `uid` | número | `id` del usuario |
| `role` | string | `buyer` o `seller` |
| `sub` | string | `id` del usuario como string |
| `iss` | string | `pinolrent-api` (siempre) |
| `aud` | string | `pinolrent-api` (siempre) |
| `jti` | string | identificador único de 32 hex chars (16 bytes random) |
| `iat` | número | timestamp de emisión (segundos UNIX) |
| `exp` | número | timestamp de expiración (iat + 24 h) |

El `jti` permite revocar un token específico antes de su `exp` natural vía
`POST /auth/logout`. La revocación inserta el `jti` en la tabla
`revoked_tokens`; el middleware de auth la consulta en cada request
protegida.

El parser rechaza explícitamente tokens con:

- Algoritmo distinto de `HS256` (incluido el clásico `alg=none`).
- Falta de `exp`, `iss`, `aud` o `jti`.
- Firma inválida, secret desconocido, `exp` vencido, o `jti` presente en
  `revoked_tokens`.

> Si tu cliente quiere decodificar el token para, por ejemplo, mostrar el
> tiempo restante o el rol sin llamar a `/auth/me`, podés usar el header
> `Authorization: Bearer <token>` y parsear el payload (no la firma) con
> cualquier librería JWT estándar. La verificación de firma la hace el
> servidor en cada request.

---

**Nota:** `/auth/*` está rate-limitado por IP (30 req/60 s → `429`).