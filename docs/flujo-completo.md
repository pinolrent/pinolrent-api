# Flujo completo

Recorrido con `curl`: un **vendedor** publica un auto, un **comprador** lo reserva y paga, y el vendedor lo confirma. Las respuestas son las reales de la API.

Necesitas:

- Server en `http://localhost:8080` (ver [Configuración](configuracion.md)).
- `jq` para leer las respuestas.

```sh
BASE=http://localhost:8080
```

## 1. Registrar comprador y vendedor

```sh
curl -s -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"compra@example.com","password":"secret123"}'

curl -s -X POST "$BASE/auth/register/seller" \
  -H 'Content-Type: application/json' \
  -d '{"email":"vende@example.com","password":"secret123"}'
```

En ambos casos (`201`):

```json
{"id":4,"email":"vende@example.com"}
```

## 2. Entrar y guardar los tokens

```sh
SELLER=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"vende@example.com","password":"secret123"}' | jq -r .token)

BUYER=$(curl -s -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"compra@example.com","password":"secret123"}' | jq -r .token)
```

Cada login devuelve (`200`):

```json
{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}
```

Puedes ver tu perfil con:

```sh
curl -s "$BASE/auth/me" -H "Authorization: Bearer $BUYER"
# {"id":4,"email":"compra@example.com","role":"buyer"}
```

## 3. El vendedor publica un auto

```sh
curl -s -X POST "$BASE/seller/cars" \
  -H "Authorization: Bearer $SELLER" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Toyota Yaris","price_per_day":45000,"photo_url":"https://example.com/yaris.jpg"}'
```

Recibes (`201`) — queda ligado a tu `owner_id`:

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

## 4. El comprador reserva

```sh
curl -s -X POST "$BASE/reservations" \
  -H "Authorization: Bearer $BUYER" \
  -H 'Content-Type: application/json' \
  -d '{"car_id":1,"start_date":"2026-10-01","end_date":"2026-10-05"}'
```

Recibes (`201`) — aún sin pago, por eso no viene `payment`:

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

## 5. El comprador paga

```sh
curl -s -X POST "$BASE/reservations/1/payment" \
  -H "Authorization: Bearer $BUYER" \
  -H 'Content-Type: application/json' \
  -d '{"method":"pos","proof_url":"https://example.com/boleta.pdf"}'
```

Recibes (`201`):

```json
{
  "id":1,
  "reservation_id":1,
  "method":"pos",
  "status":"pending",
  "proof_url":"https://example.com/boleta.pdf"
}
```

Si intentas pagar de nuevo la misma reserva → `409 {"error":"payment already recorded"}`.

## 6. El vendedor confirma

```sh
curl -s -X PATCH "$BASE/seller/reservations/1/confirm" \
  -H "Authorization: Bearer $SELLER"
```

Recibes (`200`) — queda `confirmed` y el pago `approved`, todo en una transacción:

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

## Cancelar (antes de pagar)

Mientras esté `pending` y **sin pago**, el comprador puede cancelar:

```sh
curl -s -X PATCH "$BASE/reservations/1/cancel" -H "Authorization: Bearer $BUYER"
```

Recibes (`200`) — pasa a `cancelled` y esas fechas quedan libres de nuevo:

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

Si ya tiene pago, no deja:

```sh
curl -s -o /dev/null -w '%{http_code}\n' \
  -X PATCH "$BASE/reservations/2/cancel" -H "Authorization: Bearer $BUYER"
# → 409 {"error":"payment already recorded, cannot cancel"}
```

## Cada vendedor solo ve lo suyo

Si `vende@example.com` intenta tocar el auto de otro:

```sh
curl -s -o /dev/null -w '%{http_code}\n' \
  -X PATCH "$BASE/seller/cars/2" -H "Authorization: Bearer $SELLER" \
  -H 'Content-Type: application/json' -d '{"active":false}'
# → 404 {car not found} (existe, pero no es tuyo)
```

## Qué queda al final

- El auto 1 ya no aparece en `GET /cars?start_date=2026-10-01&end_date=2026-10-05` → `[]`.
- El vendedor ve sus reservas en `GET /seller/reservations`. Las listas aceptan `limit`/`offset` (por defecto 50, máximo 200, `offset` máx. 10000) y `GET /cars` filtra por `owner_id`.
- Este mismo flujo está automatizado en `scripts/demo.sh` (`make demo`), que además prueba: body muy grande (`413`), fechas pasadas (`400`), choque de fechas (`409`), más de 30 días (`400`), paginación mal (`400`), cancelar y re-reservar, comprador sin permiso de vendedor (`403`), límite de intentos (`429`), falta de `JWT_SECRET` y apagado limpio con `SIGTERM`.
