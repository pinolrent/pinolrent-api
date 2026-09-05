# Reservas

Todas necesitan `Authorization: Bearer <token>`.

## `POST /reservations`

Reserva un auto. Necesita login.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `car_id` | número | sí | debe existir y estar activo |
| `start_date` | `YYYY-MM-DD` | sí | no puede ser anterior a hoy (UTC) |
| `end_date` | `YYYY-MM-DD` | sí | `>= start_date`, máximo 30 días de rango |

```json
{"car_id":1,"start_date":"2026-10-01","end_date":"2026-10-05"}
```

Si dos personas intentan reservar el mismo auto en las mismas fechas, solo una pasa (usa transacción).

**Responde** `201` (con el auto incluido, sin pago todavía):

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}
}
```

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid start_date, expected YYYY-MM-DD` | Formato mal |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Formato mal |
| `400` | `end_date must be on or after start_date` | Rango al revés |
| `400` | `start_date cannot be in the past` | Fecha pasada |
| `400` | `reservation cannot be longer than 30 days` | Más de 30 días |
| `400` | `car_id is required` | Falta o es 0 |
| `404` | `car not found` | No existe |
| `409` | `car is not active` | Existe pero está apagado |
| `409` | `car already reserved for the requested dates` | Ya hay reserva en esas fechas |
| `400` | `invalid JSON body` | JSON roto o campos desconocidos |
| `413` | `request body too large` | Más de 1 MB |
| `401` | ver [00-general](00-general.md) | Sin token |

---

## `GET /reservations`

Tus reservas, más nuevas primero. Acepta `limit`/`offset`.

```json
[
  {
    "id":1,"user_id":3,"car_id":1,
    "start_date":"2026-10-01","end_date":"2026-10-05",
    "status":"confirmed",
    "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true},
    "payment":{"id":1,"reservation_id":1,"method":"pos","status":"approved","proof_url":"https://..."}
  }
]
```

Sin reservas → `[]`.

---

## `GET /reservations/{id}`

Detalle de una reserva. La puede ver el comprador que la hizo o el vendedor dueño del auto.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `404` | `reservation not found` | No existe o no es tuya |

---

## `PATCH /reservations/{id}/cancel`

Cancela tu reserva. Necesita login.

Solo si: es tuya, está `pending` y **no tiene pago** (si ya pagaste, hablá con el vendedor).

**Responde** `200` con la reserva ya `cancelled`.

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `404` | `reservation not found` | No existe o no es tuya |
| `409` | `reservation is not pending` | Ya confirmada o cancelada |
| `409` | `payment already recorded, cannot cancel` | Ya tiene pago |

Al cancelar, esas fechas vuelven a estar disponibles.

---

## `GET /seller/reservations`

Reservas de tus autos como vendedor, más nuevas primero. Necesita ser `seller`. Acepta `limit`/`offset`.

Array de reservas. Sin reservas → `[]`.
