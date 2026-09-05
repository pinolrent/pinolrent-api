# Pinol Rent API

API para **renta de autos entre particulares**. Unos usuarios publican sus autos (vendedores), otros los reservan y pagan (compradores). Hecha en **Go** con **SQLite**.

## Arranque rápido

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api
```

Para desarrollo (usa valores por defecto y no necesitas `.env`):

```sh
make tools   # solo la primera vez
make dev     # levanta el server
make watch   # levanta con recarga automática al editar
```

## Endpoints

| Método | Ruta | ¿Necesita login? | Para qué sirve |
|--------|------|-----------------|----------------|
| GET | `/health` | no | Ver si el server y la base están bien |
| POST | `/auth/register` · `/auth/register/seller` | no | Crear cuenta de comprador / vendedor |
| POST | `/auth/login` | no | Entrar y obtener un token (dura 24 h) |
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
- Auth con **JWT HS256** (24 h) y **bcrypt** para contraseñas.
- `price_per_day` va en **centavos** (ej. 45000 = $450). Fechas como `YYYY-MM-DD`.
- Cada request con body no puede pasar de **1 MB**. JSON con campos desconocidos da error.
- Login y registro limitados a **30 intentos por minuto por IP**. CORS abierto por defecto (se puede cerrar con `CORS_ALLOWED_ORIGINS`).
- Listas paginadas con `limit`/`offset` (por defecto 50, máximo 200). Reservas de máximo **30 días**.
- Límites: `email` hasta 254 caracteres, `password` 8-72, `name` del auto hasta 200, URLs hasta 2048.
- `JWT_SECRET` debe tener al menos 32 caracteres o el server no arranca. El token exige `iss`, `aud` y `exp`; el `jti` solo se usa para cerrar sesión.
- `GET /health` responde la versión del binario (`make build` la inyecta).

Docs en español, comentarios del código en inglés.
