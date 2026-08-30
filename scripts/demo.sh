#!/usr/bin/env bash
# Self-contained end-to-end smoke test. Builds the API, boots it on a
# temporary port with a throwaway database, exercises the seller/buyer flow
# and asserts the hardened edge cases. Exits non-zero on any failure.
set -u

cd "$(dirname "$0")/.."

PORT="${PORT:-8132}"
BASE="http://localhost:$PORT"
DB="$(mktemp /tmp/pinolrent-demo.XXXXXX.db)"
LOG="$(mktemp /tmp/pinolrent-demo.XXXXXX.log)"
BIN="$(mktemp /tmp/pinolrent-demo.XXXXXX.bin)"
PID=""

failures=0
pass=0

check() {
  local desc="$1" expected="$2" got="$3"
  if [ "$expected" = "$got" ]; then
    pass=$((pass + 1))
    printf '  ok   %s (%s)\n' "$desc" "$got"
  else
    failures=$((failures + 1))
    printf '  FAIL %s esperado=%s obtenido=%s\n' "$desc" "$expected" "$got"
  fi
}

cond() {
  local desc="$1" rc="$2"
  if [ "$rc" -eq 0 ]; then
    pass=$((pass + 1))
    printf '  ok   %s\n' "$desc"
  else
    failures=$((failures + 1))
    printf '  FAIL %s\n' "$desc"
  fi
}

cleanup() {
  [ -n "$PID" ] && kill -9 "$PID" 2>/dev/null
  rm -f "$DB" "$DB-shm" "$DB-wal" "$LOG" "$BIN"
}
trap cleanup EXIT INT TERM

echo "== build =="
go build -o "$BIN" ./cmd/api || { echo "build FAIL"; exit 1; }

echo "== start =="
DATABASE_URL="$DB" JWT_SECRET=demo-secret-not-for-production PORT="$PORT" \
  "$BIN" > "$LOG" 2>&1 &
PID=$!
sleep 1
if ! curl -sf "$BASE/health" > /dev/null; then
  echo "server no levantó:"; cat "$LOG"; exit 1
fi

echo "== health con version =="
health_version=$(curl -s "$BASE/health" | jq -r '.version // empty')
check "health incluye version (dev)" "dev" "$health_version"

echo "== fail-fast sin JWT_SECRET =="
DATABASE_URL="$DB" PORT=9999 "$BIN" > /dev/null 2>&1
check "aborta sin JWT_SECRET" "1" "$?"

echo "== auth =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}')
check "registro comprador -> 201" "201" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register/seller" \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123"}')
check "registro vendedor -> 201" "201" "$code"

buyer=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}' | jq -r .token)
seller=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123"}' | jq -r .token)
[ -n "$buyer" ] && [ "$buyer" != "null" ]; cond "token comprador" $?
[ -n "$seller" ] && [ "$seller" != "null" ]; cond "token vendedor" $?

echo "== seller =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" \
  -H "Authorization: Bearer $buyer" -H 'Content-Type: application/json' \
  -d '{"name":"X","price_per_day":1}')
check "comprador no crea autos -> 403" "403" "$code"

car_json=$(curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Honda Fit","price_per_day":25000,"photo_url":"https://example.com/fit.jpg"}')
car=$(printf '%s' "$car_json" | jq -r .id)
car_owner=$(printf '%s' "$car_json" | jq -r .owner_id)
check "crear auto" "numero" "$([ "$car" != "null" ] && echo numero)"
curl -s -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":true}' > /dev/null

echo "== reservas =="
res=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-01-10\",\"end_date\":\"2027-01-12\"}" | jq -r .id)
check "crear reserva" "numero" "$([ "$res" != "null" ] && echo numero)"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-01-10\",\"end_date\":\"2027-01-12\"}")
check "overlap -> 409" "409" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2020-01-01\",\"end_date\":\"2020-01-02\"}")
check "fecha pasada -> 400" "400" "$code"

echo "== pagos =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations/$res/payment" \
  -H "Authorization: Bearer $buyer" -H 'Content-Type: application/json' \
  -d '{"method":"pos","proof_url":"https://example.com/recibo.jpg"}')
check "registrar pago -> 201" "201" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/seller/reservations/$res/confirm" \
  -H "Authorization: Bearer $seller")
check "confirmar -> 200" "200" "$code"

echo "== cancelación =="
res2=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-02-01\",\"end_date\":\"2027-02-03\"}" | jq -r .id)
check "reserva para cancelar" "numero" "$([ "$res2" != "null" ] && echo numero)"
status=$(curl -s -X PATCH "$BASE/reservations/$res2/cancel" -H "Authorization: Bearer $buyer" | jq -r .status)
check "cancelar -> cancelled" "cancelled" "$status"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/reservations/$res2/cancel" -H "Authorization: Bearer $buyer")
check "re-cancelar -> 409" "409" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-02-01\",\"end_date\":\"2027-02-03\"}")
check "re-reservar rango liberado -> 201" "201" "$code"

echo "== límite de días =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-03-01\",\"end_date\":\"2027-04-05\"}")
check "rango > 30 días -> 400" "400" "$code"

echo "== catálogo: dueño y paginación =="
n=$(curl -s "$BASE/cars?owner_id=$car_owner" | jq 'length')
check "filtro owner_id" "1" "$n"
n=$(curl -s "$BASE/cars?owner_id=999999" | jq 'length')
check "owner inexistente -> vacío" "0" "$n"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars?owner_id=abc")
check "owner_id inválido -> 400" "400" "$code"
n=$(curl -s "$BASE/cars?limit=1" | jq 'length')
check "limit=1" "1" "$n"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars?limit=0")
check "limit inválido -> 400" "400" "$code"

echo "== detalle de auto =="
res=$(curl -s "$BASE/cars/$car")
check "detalle auto -> id" "$car" "$(printf '%s' "$res" | jq -r .id)"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/999999")
check "detalle inexistente -> 404" "404" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/abc")
check "detalle id inválido -> 400" "400" "$code"
curl -s -o /dev/null -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":false}'
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/$car")
check "detalle inactivo -> 404" "404" "$code"
curl -s -o /dev/null -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":true}'

echo "== /auth/me =="
me=$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $seller")
check "auth/me rol seller" "seller" "$(printf '%s' "$me" | jq -r .role)"
check "auth/me email" "seller@example.com" "$(printf '%s' "$me" | jq -r .email)"

echo "== reglas de hardening =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"X","price_per_day":1,"photo_url":"mailto:a@b.c"}')
check "photo_url mailto -> 400" "400" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"X","price_per_day":100000001}')
check "precio > tope -> 400" "400" "$code"

pad=$(head -c 1048600 /dev/zero | tr '\0' 'a')
{ printf '{"email":"'; printf '%s' "$pad"; printf '@a.io","password":"secret123"}'; } > /tmp/.pinolrent-big.json
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' --data-binary @/tmp/.pinolrent-big.json)
rm -f /tmp/.pinolrent-big.json
check "body > 1MB -> 413" "413" "$code"

echo "== rate limit /auth/* =="
blocked=0
for _ in $(seq 1 35); do
  c=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/login" \
    -H 'Content-Type: application/json' -d '{"email":"x@x.com","password":"y"}')
  [ "$c" = "429" ] && blocked=$((blocked + 1))
done
[ "$blocked" -gt 0 ]; cond "rate limit bloquea (429 x $blocked)" $?

echo "== graceful shutdown =="
kill -TERM "$PID"
sleep 1
kill -0 "$PID" 2>/dev/null && check "se detiene con SIGTERM" "si" "no" || check "se detiene con SIGTERM" "si" "si"
PID=""

echo
echo "PASS: $pass  FAIL: $failures"
[ "$failures" -eq 0 ] || exit 1