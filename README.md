# Pinol Rent API

API para **renta de autos entre particulares**. Unos usuarios publican sus autos (vendedores), otros los reservan y pagan (compradores). Hecha en **Go** con **SQLite**.

## Arranque rápido

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api
```

Para desarrollo (usa valores por defecto y no necesitas `.env`):

```sh
make tools   # solo la primera vez (instala air, govulncheck, golangci-lint en ~/go/bin — agrégalo a tu PATH)
make dev     # levanta el server con defaults de dev (JWT_SECRET auto, sin fallar)
make run     # levanta sin defaults (falla sin JWT_SECRET, como en prod)
make watch   # levanta con recarga automática al editar (requiere make tools)
```

## Endpoints

| Método | Ruta | ¿Necesita login? | Para qué sirve |
|--------|------|-----------------|----------------|
| GET | `/health` | no | Ver si el server y la base están bien |
| POST | `/auth/register` · `/auth/register/seller` | no | Crear cuenta de comprador / vendedor |
| POST | `/auth/login` | no | Entrar y obtener access (15 min) + refresh (7 días) |
| POST | `/auth/refresh` | no | Renovar el par con un refresh de un solo uso |
| GET | `/auth/me` | sí | Ver tu propio perfil |
| POST | `/auth/logout` | sí | Cerrar sesión (invalida tu token actual) |
| GET | `/cars` | no | Ver autos disponibles (puedes filtrar por fechas o vendedor) |
| GET | `/cars/{id}` | no | Ver el detalle de un auto |
| GET · POST | `/seller/cars` | vendedor | Ver tus autos y agregar uno nuevo |
| PATCH | `/seller/cars/{id}` | vendedor | Activar o desactivar uno de tus autos |
| POST | `/reservations` | sí | Reservar un auto |
| GET | `/reservations` · `/reservations/{id}` | sí | Ver tus reservas |
| PATCH | `/reservations/{id}/cancel` | sí | Cancelar una reserva tuya (solo si aún no pagaste) |
| POST | `/reservations/{id}/payment` | sí | Pagar una reserva (`pos` o `cash`) |
| GET | `/seller/reservations` | vendedor | Ver reservas de tus autos |
| PATCH | `/seller/reservations/{id}/confirm` | vendedor | Confirmar una reserva y aprobar su pago |

Si intentas ver o tocar algo que no es tuyo, la API responde `404` como si no existiera.

## Roles

- **Comprador:** reserva autos, paga y ve sus reservas.
- **Vendedor:** publica sus autos y confirma las reservas de sus autos. Cada vendedor solo ve lo suyo.

## Documentación

- **[Índice de docs](docs/README.md)** — arquitectura, configuración, desarrollo y flujo paso a paso.
- **[Referencia de la API](docs/api/00-general.md)** — cómo se usa la API, autenticación y detalle de cada endpoint.

## Stack y reglas básicas

- **Go 1.26.5**, `net/http` sin framework, **SQLite** (`modernc.org/sqlite`) + migraciones `goose`.
- Auth con **JWT HS256** (access 15 min + refresh 7 días con rotación) y **bcrypt** para contraseñas.
- `price_per_day` va en **centavos** (ej. 45000 = $450). Fechas como `YYYY-MM-DD`.
- Cada request con body no puede pasar de **1 MB**. JSON con campos desconocidos da error.
- Login y registro limitados a **30 intentos por minuto por IP**; escritura (`POST /reservations`, `POST /seller/cars`, `POST /reservations/*/payment`) a **120 por minuto** (ráfaga 20). CORS abierto por defecto (se puede cerrar con `CORS_ALLOWED_ORIGINS`; con `ENV=prod` no permite `*`).
- Listas paginadas con `limit`/`offset` (por defecto 50, máximo 200, `offset` máx. 10000). Reservas de máximo **30 días**.
- Desactivar un auto con reservas futuras → `409`. Pool SQLite con `MaxIdleTime` 5 min / `MaxLifetime` 30 min. Migración 00005 agrega `CHECK(price_per_day <= 100M)` y `end_date >= start_date`.
- Límites: `email` hasta 254 caracteres, `password` 8-72, `name` del auto hasta 200, URLs hasta 2048.
- `JWT_SECRET` debe tener al menos 32 caracteres con entropía (16+ bytes distintos) o el server no arranca. El token exige `iss`, `aud` y `exp`; el `jti` se usa para cerrar sesión y rotar refresh.
- `GET /health` responde la versión del binario (`make build` la inyecta).
