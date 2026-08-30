# Flujo completo

Recorrido end-to-end con `curl`: un **vendedor** publica un auto, un
**comprador** lo reserva y paga, y el vendedor confirma. La respuesta de
ejemplo de cada paso es la que devuelve la API hoy.

Presupuestos:

- Server levantado en `http://localhost:8080` (ver [Configuración](configuracion.md)).
- `jq` disponible en el shell para extraer campos de las respuestas.

```sh
BASE=http://localhost:8080
```

## 1 · Registrar comprador y vendedor

```sh
curl -s -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"compra@example.com","password":"secret123"}'

curl -s -X POST "$BASE/auth/register/seller" \
  -H 'Content-Type: application/json' \
  -d '{"email":"vende@example.com","password":"secret123"}'
```

Recibido en ambos casos (`201 Created`):

```json
{"id":4,"email":"vende@example.com"}
```

## 2 · Logins

```sh
SELLER=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"vende@example.com","password":"secret123"}' | jq -r .token)

BUYER=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"compra@example.com","password":"secret123"}' | jq -r .token)
```

Cada login devuelve (`200 OK`):

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

Con el token, el perfil se consulta en `GET /auth/me`:

```sh
curl -s "$BASE/auth/me" -H "Authorization: Bearer $BUYER"
# {"id":4,"email":"compra@example.com","role":"buyer"}
```

## 3 · El vendedor crea un auto

```sh
curl -s -X POST "$BASE/seller/cars" \
  -H "Authorization: Bearer $SELLER" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Toyota Yaris","price_per_day":45000,"photo_url":"https://example.com/yaris.jpg"}'
```

Recibido (`201 Created`) — el auto queda ligado a tu `owner_id`:

```json
{
  "id":1,
  "owner_id":4,
  "name":"Toyota Yaris",
  "photo_url":"https://example.com/yaris.jpg",
  "price_per_day":45000,
  "active":true
}
```

## 4 · El comprador reserva

```sh
curl -s -X POST "$BASE/reservations" \
  -H "Authorization: Bearer $BUYER" \
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
    "owner_id":4,
    "name":"Toyota Yaris",
    "price_per_day":45000,
    "active":true
  }
}
```

## 5 · El comprador paga

```sh
curl -s -X POST "$BASE/reservations/1/payment" \
  -H "Authorization: Bearer $BUYER" \
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

## 6 · El vendedor confirma

```sh
curl -s -X PATCH "$BASE/seller/reservations/1/confirm" \
  -H "Authorization: Bearer $SELLER"
```

Recibido (`200 OK`) — la reserva queda `confirmed` y el pago `approved`, todo
en una transacción:

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
    "owner_id":4,
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

## Cancelación (antes de pagar)

Mientras una reserva esté `pending` y **sin pago**, el comprador la cancela;

```sh
curl -s -X PATCH "$BASE/reservations/1/cancel" -H "Authorization: Bearer $BUYER"
```

Recibido (`200 OK`) — la reserva queda `cancelled` y su rango vuelve a estar
disponible:

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"cancelled",
  "car":{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}
}
```

Una reserva **con pago registrado** no se cancela por API:

```sh
curl -s -o /dev/null -w '%{http_code}\n' \
  -X PATCH "$BASE/reservations/2/cancel" -H "Authorization: Bearer $BUYER"
# → 409 {"error":"payment already recorded, cannot cancel"}
```

## Aislamiento entre vendedores

`vende@example.com` no puede tocar los autos de otro vendedor:

```sh
# otro vendedor crea su auto y vos intentás desactivarlo
curl -s -o /dev/null -w '%{http_code}\n' \
  -X PATCH "$BASE/seller/cars/2" -H "Authorization: Bearer $SELLER" \
  -H 'Content-Type: application/json' -d '{"active":false}'
# → 404 {car not found} (existe pero no es tuyo)
```

## Estado final

- El auto 1 ya no aparece al listar con el rango reservado:
  `GET /cars?start_date=2026-10-01&end_date=2026-10-05` → `[]`.
- El vendedor ve sus reservas en `GET /seller/reservations`. Las listas
  aceptan `limit`/`offset` (default 50, máx 200) y `GET /cars` filtra por
  `owner_id`.
- El mismo flujo está automatizado en `scripts/demo.sh` (`make demo`), que
  además comprueba hardening: body demasiado grande (`413`), fechas pasadas
  (`400`), overlap (`409`), rango > 30 días (`400`), paginación inválida
  (`400`), cancelación y re-reserva, comprador sin permisos de vendedor
  (`403`) y rate limit sobre `/auth/*` (`429`).