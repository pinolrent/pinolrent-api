# Pagos

## `POST /reservations/{id}/payment`

Registra el pago de tu reserva. Necesita login.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `method` | texto | sí | `pos` o `cash` |
| `proof_url` | texto | no | si va, URL `http(s)` hasta 2048 |

```json
{"method":"pos","proof_url":"https://example.com/boleta.pdf"}
```

Reglas: la reserva debe existir y ser tuya, no estar `cancelled` y no tener ya un pago (uno por reserva).

**Responde** `201` (nace `pending`):

```json
{
  "id":1,
  "reservation_id":1,
  "method":"pos",
  "status":"pending",
  "proof_url":"https://example.com/boleta.pdf"
}
```

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `400` | `method must be pos or cash` | Método desconocido |
| `400` | `proof_url is too long` | Más de 2048 |
| `400` | `invalid proof_url` | URL mal formada o sin `http(s)` |
| `404` | `reservation not found` | No existe o no es tuya |
| `409` | `reservation is not pending` | Está cancelada o confirmada |
| `409` | `payment already recorded` | Ya tiene pago |
| `400` | `invalid JSON body` | JSON roto o campos desconocidos |
| `413` | `request body too large` | Más de 1 MB |

---

## `PATCH /seller/reservations/{id}/confirm`

El vendedor aprueba el pago y confirma la reserva, todo junto en una transacción. Necesita ser `seller` y que el auto sea tuyo. Sin body.

Pasa `payments.status` → `approved` y `reservations.status` → `confirmed`.

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

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es número |
| `404` | `reservation not found` | No existe o el auto no es tuyo |
| `409` | `reservation is not pending` | Ya confirmada o cancelada |
| `409` | `no payment recorded for this reservation` | No hay pago que aprobar |
