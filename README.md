# Pinol Rent API

API para renta de autos escrita en Go (stdlib) con SQLite. MVP P0: clientes pueden ver autos disponibles, reservar, pagar y consultar el estado; admins publican/activan autos y confirman pagos y reservas.

## Stack

- Go 1.26, `net/http` (stdlib)
- SQLite via `modernc.org/sqlite` (`database/sql`)
- JWT HS256 (`golang-jwt/jwt/v5`), bcrypt (`golang.org/x/crypto`)

## Variables de entorno

| Variable | Default | Requerida | Descripción |
|----------|---------|-----------|-------------|
| `PORT` | `8080` | no | Puerto del servidor |
| `DATABASE_URL` | `pinolrent.db` | no | Ruta/DSN del archivo SQLite |
| `JWT_SECRET` | — | **sí** | Secreto HS256 para firmar tokens (el proceso aborta si falta) |
| `ADMIN_EMAIL` | `admin@pinolrent.com` | no | Email del admin que se siembra al arrancar |
| `ADMIN_PASSWORD` | — | **sí** | Password del admin (el proceso aborta si falta) |

> El admin se crea al arrancar si no existe. Si ya existe, su password se actualiza con `ADMIN_PASSWORD`. Si ese email ya pertenece a una cuenta `client`, el arranque falla.

## Ejecutar

```sh
export JWT_SECRET="cambia-este-secreto"
export ADMIN_PASSWORD="clave-del-admin"
go run ./cmd/api
```

### Proveerse un secreto aleatorio

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
```

## Endpoints

| Método | Ruta | Auth | Descripción |
|--------|------|------|-------------|
| GET | `/health` | no | Healthcheck |
| POST | `/auth/register` | no (rate-limited) | Crea un cliente `{email, password}` |
| POST | `/auth/login` | no (rate-limited) | Devuelve `{token}` |
| GET | `/cars?start_date=&end_date=` | no | Autos activos; con fechas, excluye los reservados en `[start_date, end_date]` (ISO `YYYY-MM-DD`) |
| POST | `/admin/cars` | admin | Alta de auto `{name, photo_url?, price_per_day}` |
| PATCH | `/admin/cars/{id}` | admin | Activa/inactiva `{active}` |
| POST | `/reservations` | client | Crea reserva `{car_id, start_date, end_date}` (solapamiento rechazado con 409) |
| GET | `/reservations` | client | Mis reservas (incluye auto y pago) |
| GET | `/reservations/{id}` | client/admin | Detalle (client ve solo las suyas) |
| POST | `/reservations/{id}/payment` | client | Registra pago `{method: pos\|cash, proof_url?}` (uno por reserva) |
| PATCH | `/admin/reservations/{id}/confirm` | admin | Aprueba pago y confirma reserva (`confirmed`) |

Auth: cabecera `Authorization: Bearer <token>`.

Reglas:
- Disponibilidad: `cars.active=1` y sin reservas (`pending`/`confirmed`) solapadas en `[start_date, end_date]`.
- `start_date` no puede ser anterior a hoy; `end_date` >= `start_date`.
- Un auto puede tener muchas reservas pero solo un pago.
- Body limitado a 1 MB. `price_per_day` en centavos (0..100 000 000).
- Rate limit por IP: 30 req/60 s sobre `/auth/*`.

Estados: reserva `pending` → `confirmed`; pago `pending` → `approved`.

## Tests

```sh
go test ./...
go vet ./...
```

## Fuera de alcance

Uploads reales, gateway POS, WhatsApp API, paginación, frontend, rate limiting distribuido (IP es `RemoteAddr`).