# Pinol Rent API

API para renta de autos escrita en **Go (stdlib)** con **SQLite**. Clientes ven
autos disponibles, reservan, pagan y consultan el estado; admins publican autos
y confirman pagos y reservas.

## Quickstart

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
export ADMIN_PASSWORD="clave-del-admin"
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
| POST | `/auth/register` · `/auth/login` | no | Cuenta de cliente y login (JWT 24 h) |
| GET | `/cars` | no | Autos activos; con `start_date`/`end_date` excluye reservados |
| POST · PATCH | `/admin/cars` · `/admin/cars/{id}` | admin | Alta y activar/inactivar |
| POST | `/reservations` | client | Crear reserva (overlap → 409) |
| GET | `/reservations` · `/reservations/{id}` | client/admin | Mis reservas y detalle |
| POST | `/reservations/{id}/payment` | client | Registrar pago (`pos`\|`cash`) |
| PATCH | `/admin/reservations/{id}/confirm` | admin | Aprobar pago y confirmar |

## Documentación

- **[Índice de la documentación](docs/README.md)** — arquitectura, configuración, desarrollo y flujo completo.
- **[Referencia de la API](docs/api/00-general.md)** — convenciones, auth, errores y cada endpoint con payloads.

## Stack

Go 1.26 · `net/http` (stdlib) · SQLite (`modernc.org/sqlite`) · JWT HS256 ·
bcrypt. Notas: `price_per_day` en **centavos**, fechas ISO `YYYY-MM-DD`,
body máx. 1 MB, rate limit por IP en `/auth/*` (30 req/60 s).

Convenciones del repo: documentación en español, comentarios del código en
inglés.