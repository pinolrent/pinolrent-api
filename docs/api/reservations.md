# Reservas

Todas las rutas de reserva requieren `Authorization: Bearer <token>` (rol
`buyer` o `seller`).

## `POST /reservations`

Crea una reserva. Requiere login.

**Body:**

| Campo | Tipo | Requerido | Reglas |
|-------|------|-----------|--------|
| `car_id` | entero | sí | debe existir y estar activo |
| `start_date` | `YYYY-MM-DD` | sí | no puede ser anterior a hoy (UTC) |
| `end_date` | `YYYY-MM-DD` | sí | `>= start_date` |

```json
{"car_id":1,"start_date":"2026-10-01","end_date":"2026-10-05"}
```

La operación corre en una transacción (`BEGIN IMMEDIATE`): verifica que el auto
exista y esté activo, que no solape con reservas `pending`/`confirmed` y
rechaza con `409` si algo falla. Un vendedor también puede reservar el auto de
otro.

**Respuesta** — `201 Created` (reservation view; `payment` ausente):

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

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid start_date, expected YYYY-MM-DD` | Formato inválido |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Formato inválido |
| `400` | `end_date must be on or after start_date` | Rango invertido |
| `400` | `start_date cannot be in the past` | Fecha anterior a hoy |
| `400` | `car_id is required` | Campo ausente (o `0`) |
| `404` | `car not found` | El auto no existe |
| `409` | `car is not active` | El auto existe pero está inactivo |
| `409` | `car already reserved for the requested dates` | Solapa con otra reserva no cancelada |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |
| `401` | ver [00-general](00-general.md) | Sin token / token inválido |

---

## `GET /reservations`

Lista las reservas del comprador autenticado, más recientes primero.

**Respuesta** — `200 OK`:

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

Detalle de una reserva. Un comprador solo ve las suyas; un vendedor ve las
reservas de **sus** autos.

**Respuesta** — `200 OK` (misma shape que el ejemplo anterior).

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es un entero |
| `404` | `reservation not found` | No existe, o no te pertenece ni al comprador ni al vendedor dueño (se oculta) |

---

## `GET /seller/reservations`

Lista las reservas de los autos del vendedor autenticado, más recientes
primero. Requiere rol `seller`.

**Respuesta** — `200 OK` (array de reservation views). Sin reservas → `[]`.