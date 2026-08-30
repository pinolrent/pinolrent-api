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
| GET | `/cars` | no | Autos activos; con `start_date`/`end_date` excluye reservados |
| GET · POST | `/seller/cars` · `/seller/cars` | seller | Mis autos y alta |
| PATCH | `/seller/cars/{id}` | seller | Activar/inactivar (solo tu auto) |
| POST | `/reservations` | login | Crear reserva (overlap → 409) |
| GET | `/reservations` · `/reservations/{id}` | login | Mis reservas y detalle |
| POST | `/reservations/{id}/payment` | login | Registrar pago (`pos`\|`cash`) |
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

Go 1.26 · `net/http` (stdlib) · SQLite (`modernc.org/sqlite`) · JWT HS256 ·
bcrypt. Notas: `price_per_day` en **centavos**, fechas ISO `YYYY-MM-DD`,
body máx. 1 MB, rate limit por IP en `/auth/*` (30 req/60 s).

Convenciones del repo: documentación en español, comentarios del código en
inglés.