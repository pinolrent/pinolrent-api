# Flujo completo

Recorrido end-to-end con `curl`: de la mano del cliente que se registra hasta el
admin que confirma la reserva. La respuesta de ejemplo de cada paso es la que
devuelve la API hoy.

Presupuestos:

- Server levantado en `http://localhost:8080` (ver [Configuración](configuracion.md)).
- `jq` disponible en el shell para extraer campos de las respuestas.

```sh
BASE=http://localhost:8080
```

## 1 · Registrar un cliente

```sh
curl -s -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@example.com","password":"secret123"}'
```

Enviado:

```json
{"email":"demo@example.com","password":"secret123"}
```

Recibido (`201 Created`):

```json
{"id":3,"email":"demo@example.com"}
```

## 2 · Logins

El admin se siembra al arrancar con `ADMIN_EMAIL`/`ADMIN_PASSWORD`.

```sh
ADMIN_TOKEN=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@pinolrent.com","password":"admin123"}' | jq -r .token)

CLIENT_TOKEN=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@example.com","password":"secret123"}' | jq -r .token)
```

Cada login devuelve (`200 OK`):

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

## 3 · El admin crea un auto

```sh
curl -s -X POST "$BASE/admin/cars" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Toyota Yaris","price_per_day":45000,"photo_url":"https://example.com/yaris.jpg"}'
```

Recibido (`201 Created`):

```json
{
  "id":1,
  "name":"Toyota Yaris",
  "photo_url":"https://example.com/yaris.jpg",
  "price_per_day":45000,
  "active":true
}
```

## 4 · El cliente reserva

```sh
curl -s -X POST "$BASE/reservations" \
  -H "Authorization: Bearer $CLIENT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"car_id":1,"start_date":"2026-10-01","end_date":"2026-10-05"}'
```

Recibido (`201 Created`) — el pago todavía no existe, por eso va `omitempty`:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{
    "id":1,
    "name":"Toyota Yaris",
    "price_per_day":45000,
    "active":true
  }
}
```

## 5 · El cliente paga

```sh
curl -s -X POST "$BASE/reservations/1/payment" \
  -H "Authorization: Bearer $CLIENT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"method":"pos","proof_url":"https://example.com/boleta.pdf"}'
```

Recibido (`201 Created`):

```json
{
  "id":1,
  "reservation_id":1,
  "method":"pos",
  "status":"pending",
  "proof_url":"https://example.com/boleta.pdf"
}
```

Un segundo pago sobre la misma reserva falla (`409 Conflict`):

```json
{"error":"payment already recorded"}
```

## 6 · El admin confirma

```sh
curl -s -X PATCH "$BASE/admin/reservations/1/confirm" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Recibido (`200 OK`) — la reserva queda `confirmed` y el pago `approved`, todo en
una transacción:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"confirmed",
  "car":{
    "id":1,
    "name":"Toyota Yaris",
    "price_per_day":45000,
    "active":true
  },
  "payment":{
    "id":1,
    "reservation_id":1,
    "method":"pos",
    "status":"approved",
    "proof_url":"https://example.com/boleta.pdf"
  }
}
```

## Estado final

- El auto 1 ya no aparece al listar con el rango reservado:
  `GET /cars?start_date=2026-10-01&end_date=2026-10-05` → `[]`.
- `GET /reservations/1` con el token del cliente devuelve el mismo detalle
  confirmado.
- El mismo flujo está automatizado en `scripts/demo.sh` (`make demo`), que
  además comprueba hardening: body demasiado grande (`413`), fechas pasadas
  (`400`), overlap (`409`) y rate limit sobre `/auth/*` (`429`).