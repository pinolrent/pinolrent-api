# Pinol Rent API — Plan MVP P0

## Objetivo
API para rentar autos. 6 funciones core.

## Stack
Go 1.26, `net/http` stdlib, SQLite (`database/sql`), JWT HS256, bcrypt.

## Funciones P0
1. Cliente: ver autos disponibles (foto, precio/día, filtrado por fechas).
2. Cliente: reservar auto por rango de fechas.
3. Cliente: pagar — elige `pos` o `cash` y registra intento.
4. Cliente: ver estado de reserva (`pending`/`confirmed`).
5. Admin: publicar y activar/inactivar autos.
6. Admin: confirmar pago y reserva.

## Modelo de datos (SQLite)
- `users(id, email, password_hash, role)` — `role` en `client,admin`
- `cars(id, name, photo_url, price_per_day, active)`
- `reservations(id, user_id, car_id, start_date, end_date, status)`
- `payments(id, reservation_id, method, status, proof_url)` — `method` en `pos,cash`

Disponibilidad: `cars.active=1` y sin reservas solapadas en `[start_date, end_date]`.

## API
| Método | Ruta | Auth | Descripción |
|--------|------|------|-------------|
| GET | /health | no | healthcheck |
| POST | /auth/register | no | crea cliente |
| POST | /auth/login | no | retorna JWT |
| GET | /cars?start_date=&end_date= | no | lista disponibles |
| POST | /admin/cars | admin | alta auto |
| PATCH | /admin/cars/{id} | admin | activa/inactiva |
| POST | /reservations | client | crea reserva |
| GET | /reservations | client | mis reservas |
| GET | /reservations/{id} | client/admin | detalle |
| POST | /reservations/{id}/payment | client | registra pago |
| PATCH | /admin/reservations/{id}/confirm | admin | confirma pago+reserva |

## Fases
1. **DB + Auth** — SQLite, migrate, register/login, middleware JWT, seed admin por env.
2. **Cars** — CRUD admin + `GET /cars` con filtro de fechas.
3. **Reservations** — creación con control de solapamiento (tx `BEGIN IMMEDIATE`), listado y detalle.
4. **Payments** — registro de intento y confirmación admin.

## Config
`PORT` (default 8080), `DATABASE_URL` (default `pinolrent.db`), `JWT_SECRET`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`.

## Fuera de P0
Uploads reales, gateway POS, WhatsApp API, paginación, frontend.
