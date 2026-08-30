# Pagos

## `POST /reservations/{id}/payment`

Registra el pago de la reserva del cliente autenticado. Requiere rol `client`.

**Body:**

| Campo | Tipo | Requerido | Reglas |
|-------|------|-----------|--------|
| `method` | string | sí | `pos` \| `cash` |
| `proof_url` | string | no | si se manda, URL `http(s)` válida |

```json
{"method":"pos","proof_url":"https://example.com/boleta.pdf"}
```

Reglas: la reserva debe existir y **pertenecer al cliente**; no puede estar
`cancelled`; y no puede tener ya un pago (uno por reserva).

**Respuesta** — `201 Created` (nace `status: pending`):

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

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es un entero |
| `400` | `method must be pos or cash` | Método desconocido |
| `400` | `invalid proof_url` | URL malformada o sin scheme `http(s)` |
| `404` | `reservation not found` | No existe, **o** pertenece a otro cliente (se oculta) |
| `409` | `cancelled reservation cannot be paid` | La reserva está cancelada |
| `409` | `payment already recorded` | La reserva ya tiene pago |
| `400` | `invalid JSON body` | JSON malformado / campos desconocidos |
| `413` | `request body too large` | Body > 1 MB |

---

## `PATCH /admin/reservations/{id}/confirm`

Aprueba el pago y confirma la reserva, atomáticamente (transacción
`BEGIN IMMEDIATE`). Requiere rol `admin`. Sin body.

Efectos: `payments.status` → `approved` y `reservations.status` → `confirmed`.

**Respuesta** — `200 OK` (reservation view ya confirmada):

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"confirmed",
  "car":{"id":1,"name":"Toyota Yaris","price_per_day":45000,"active":true},
  "payment":{"id":1,"reservation_id":1,"method":"pos","status":"approved","proof_url":"https://..."}
}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid reservation id` | `{id}` no es un entero |
| `404` | `reservation not found` | No existe la reserva |
| `409` | `reservation is not pending` | Ya fue confirmada o cancelada |
| `409` | `no payment recorded for this reservation` | No hay pago que aprobar |