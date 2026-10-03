# Administración

El rol `admin` no tiene registro self-service: la cuenta se crea o promueve con el comando de operación, que escribe directo en la misma base que usa el server:

```sh
make admin EMAIL=admin@example.com PASSWORD=secret123   # crea la cuenta si no existe
make admin EMAIL=admin@example.com                      # si ya existe, solo garantiza el rol
```

- El comando usa `DATABASE_URL` (o `pinolrent.db` por defecto) y aplica las migraciones si faltan.
- La cuenta creada nace con el rol `admin` (no es compradora). Si la cuenta ya existía, conserva sus roles y su contraseña (`PASSWORD` se ignora).
- Es idempotente: correrlo dos veces no cambia nada.

Todas las rutas de esta sección necesitan un token de una cuenta con rol `admin`; con otro rol responden `403 insufficient permissions`.

## El flujo de doble aprobación

1. El comprador reserva y registra el pago (`pending`).
2. El vendedor acepta la reserva: queda `awaiting_admin` y el pago sigue `pending` (ver [payments](payments.md)).
3. El admin la aprueba y queda `confirmed` con el pago `approved`, o la rechaza y vuelve a `pending` con un motivo para que el vendedor corrija y la acepte de nuevo.

---

## `GET /admin/reservations`

Historial completo: todas las reservas de todos los compradores y vendedores, más nuevas primero. Necesita rol `admin`. Acepta `limit`/`offset` y un filtro opcional por estado.

| Parámetro | Valor | Reglas |
|-----------|-------|--------|
| `status` | `pending`, `awaiting_admin`, `confirmed` o `cancelled` | otro valor → `400 invalid status` |

```
GET /admin/reservations?status=awaiting_admin
Authorization: Bearer <token-admin>
```

**Responde** `200` con un array de reservas (mismo formato que `GET /reservations`: cada una con su auto y su pago). Sin resultados → `[]`.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid status` | Filtro desconocido |
| `400` | `invalid limit` / `invalid offset` | Paginación mal |
| `401` | ver [00-general](00-general.md) | Sin token |
| `403` | `insufficient permissions` | Token sin rol `admin` |

---

## `PATCH /admin/reservations/{id}/approve`

Aprueba una solicitud que el vendedor ya aceptó. Sin body.

En una sola transacción: `payments.status` → `approved` y `reservations.status` → `confirmed`.

**Responde** `200`:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"confirmed",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"approved","proof_url":"https://..."}
}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `404` | `reservation not found` | No existe |
| `409` | `reservation is not awaiting approval` | No está `awaiting_admin` (p. ej. el vendedor todavía no la aceptó) |
| `409` | `no payment recorded for this reservation` | No hay pago que aprobar |
| `409` | `payment is not pending` | El pago ya no está `pending` |

---

## `PATCH /admin/reservations/{id}/reject`

Rechaza una solicitud aceptada: la reserva **vuelve a `pending`** con un motivo de corrección y el pago sigue `pending`. El vendedor corrige y la acepta de nuevo (loop), o rechaza el pago para cancelarla.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `admin_note` | texto | no | hasta 500 caracteres; queda visible en la reserva |

```json
{"admin_note":"el comprobante no coincide con el monto"}
```

**Responde** `200` con la reserva en `pending` y el motivo en `admin_note`. El motivo se limpia solo cuando el vendedor la acepta de nuevo.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `400` | `invalid JSON body` | Sin body o con campos desconocidos |
| `400` | `admin_note is too long` | Más de 500 caracteres |
| `404` | `reservation not found` | No existe |
| `409` | `reservation is not awaiting approval` | No está `awaiting_admin` |
