# Pinol Rent API

API de **renta de autos entre particulares** escrita en **Go (stdlib)** con
**SQLite**. Los vendedores publican sus autos; los compradores reservan, pagan
y consultan el estado; el vendedor confirma pagos y reservas de sus autos.

## Quickstart

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api
```

Para desarrollo con defaults y hot-reload:

```sh
make tools   # una vez
make dev     # arranca con defaults de dev (sin .env)
make watch   # hot-reload
```

## Endpoints

| Método | Ruta | Auth | Descripción |
|--------|------|------|-------------|
| GET | `/health` | no | Healthcheck |
| POST | `/auth/register` · `/auth/register/seller` | no | Registro de comprador / vendedor |
| POST | `/auth/login` | no | Login (JWT 24 h) |
| GET | `/auth/me` | login | Tu perfil (`id`, `email`, `role`) |
| GET | `/cars` | no | Autos activos; con fechas excluye reservados, con `owner_id` filtra vendedor |
| GET | `/cars/{id}` | no | Detalle de un auto activo |
| GET · POST | `/seller/cars` · `/seller/cars` | seller | Mis autos y alta |
| PATCH | `/seller/cars/{id}` | seller | Activar/inactivar (solo tu auto) |
| POST | `/reservations` | login | Crear reserva (overlap o >30 días → 409/400) |
| GET | `/reservations` · `/reservations/{id}` | login | Mis reservas y detalle |
| PATCH | `/reservations/{id}/cancel` | login | Cancelar tu reserva (si está `pending` y sin pago) |
| POST | `/reservations/{id}/payment` | login | Registrar pago (`pos`\|`cash`) |
| POST | `/auth/logout` | login | Revoca el token bearer actual (inserta su `jti` en `revoked_tokens`) |
| GET | `/seller/reservations` | seller | Reservas de tus autos |
| PATCH | `/seller/reservations/{id}/confirm` | seller | Aprobar pago y confirmar (solo tu auto) |

## Roles

- **Comprador**: registra reservas, paga y consulta sus reservas.
- **Vendedor**: publica autos propios y confirma reservas de sus autos; cada
  vendedor solo ve y opera lo suyo (lo ajeno responde `404`).

## Documentación

- **[Índice de la documentación](docs/README.md)** — arquitectura, configuración, desarrollo y flujo completo.
- **[Referencia de la API](docs/api/00-general.md)** — convenciones, auth, errores y cada endpoint con payloads.

## Stack

Go 1.26 · `net/http` (stdlib) · SQLite (`modernc.org/sqlite`) + migraciones
`goose` · JWT HS256 · bcrypt · rate limit `x/time/rate` · CORS `rs/cors`.
Notas: `price_per_day` en **centavos**, fechas ISO `YYYY-MM-DD`, body máx.
1 MB, rate limit por IP en `/auth/*` (30 req/60 s), CORS habilitado por defecto
para cualquier origen (restringible con `CORS_ALLOWED_ORIGINS`), listas
paginadas (`limit`/`offset`, default 50 · máx 200) y reservas de hasta 30 días.
Límites por campo: `email` ≤ 254, `password` 8-72, `name` de auto ≤ 200,
URLs (`photo_url`, `proof_url`) ≤ 2048. `JWT_SECRET` debe medir al menos
32 bytes (se valida al arranque) y el parser exige `iss`/`aud`/`exp` además
de la firma HS256. `GET /health` reporta versión y estado de la base; el
binario se compila con `make build` inyectando la versión
(`-ldflags "-X main.version=..."`).

Convenciones del repo: documentación en español, comentarios del código en
inglés.