#!/usr/bin/env bash
# Self-contained end-to-end smoke test. Builds the API, boots it on a
# temporary port with a throwaway database, exercises the seller/buyer flow
# and asserts the hardened edge cases. Exits non-zero on any failure.
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-8132}"
BASE="http://localhost:$PORT"
DB="$(mktemp /tmp/pinolrent-demo.XXXXXX.db)"
LOG="$(mktemp /tmp/pinolrent-demo.XXXXXX.log)"
BIN="$(mktemp /tmp/pinolrent-demo.XXXXXX.bin)"
# Empty working directory for the server process. The server loads a .env
# from its own cwd, so running it from the repo root would let a developer's
# local .env decide the configuration: the fail-fast probe below would find a
# JWT_SECRET there, start successfully and hang the script forever. From an
# empty directory the only configuration is what this script passes.
RUNDIR="$(mktemp -d /tmp/pinolrent-run.XXXXXX)"
PID=""

# Same min length the server enforces, so the smoke starts without
# tripping its own fail-fast.
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-smoke-test-secret-min-32B-ok-0123456789}"

failures=0
pass=0

check() {
  local desc="$1" expected="$2" got="$3"
  if [ "$expected" = "$got" ]; then
    pass=$((pass + 1))
    printf '  ok   %s (%s)\n' "$desc" "$got"
  else
    failures=$((failures + 1))
    printf '  FAIL %s expected=%s got=%s\n' "$desc" "$expected" "$got"
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

# wait_for_health polls /health up to N seconds, sleeping 0.2s between
# attempts. Returns 0 once the server answers 200/503, 1 on timeout.
wait_for_health() {
  local seconds="$1"
  local tries=$((seconds * 5))   # 5 attempts per second
  while [ "$tries" -gt 0 ]; do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 "$BASE/health" 2>/dev/null || true)
    if [ "$code" = "200" ] || [ "$code" = "503" ]; then
      return 0
    fi
    sleep 0.2
    tries=$((tries - 1))
  done
  return 1
}

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
      kill -0 "$PID" 2>/dev/null || break
      sleep 0.2
    done
    kill -KILL "$PID" 2>/dev/null || true
  fi
  rm -f "$DB" "$DB-shm" "$DB-wal" "$LOG" "$BIN"
  rm -rf "${UPLOAD_TMP:-}" "$RUNDIR"
}
trap cleanup EXIT INT TERM

echo "== build =="
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
if ! go build -ldflags "-X main.version=$VERSION" -o "$BIN" ./cmd/api; then
  echo "build FAIL"; exit 1
fi

echo "== fail-fast without JWT_SECRET =="
# Unset JWT_SECRET to verify the server refuses to start. env -u ensures
# the variable is not inherited from the caller's environment. The
# server logs its config error to stdout (via slog), so we just check
# the non-zero exit code here; checking the message text is brittle
# because slog's output destination depends on configuration.
set +e
( cd "$RUNDIR" && exec env -u JWT_SECRET DATABASE_URL="$DB" PORT=9999 "$BIN" ) >/dev/null 2>&1
rc=$?
set -e
check "aborts without JWT_SECRET" "1" "$rc"

echo "== start =="
UPLOAD_TMP="$(mktemp -d /tmp/pinolrent-uploads.XXXXXX)"
( cd "$RUNDIR" && exec env DATABASE_URL="$DB" JWT_SECRET="$SMOKE_JWT_SECRET" PORT="$PORT" \
  UPLOAD_DIR="$UPLOAD_TMP" ADMIN_EMAILS=admin@example.com "$BIN" ) > "$LOG" 2>&1 &
PID=$!
if ! wait_for_health 5; then
  echo "server did not start in 5s:"; cat "$LOG"; exit 1
fi
check "health answers 200" "200" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health")"

echo "== health with version =="
health_version=$(curl -s "$BASE/health" | jq -r '.version // empty')
[ -n "$health_version" ]; cond "health includes version ($health_version)" $?

echo "== auth =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}')
check "register buyer -> 201" "201" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123","phone":"+56912345678"}')
check "register seller -> 201" "201" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"no-phone@example.com","password":"secret123","phone":"not-a-number"}')
check "register with invalid phone -> 400" "400" "$code"

buyer=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"buyer@example.com","password":"secret123"}' | jq -r .token)
seller=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"secret123"}' | jq -r .token)
[ -n "$buyer" ] && [ "$buyer" != "null" ]; cond "buyer token" $?
[ -n "$seller" ] && [ "$seller" != "null" ]; cond "seller token" $?

echo "== seller cars =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" \
  -H "Authorization: Bearer $buyer" -H 'Content-Type: application/json' \
  -d '{"name":"X","price_per_day":1}')
check "buyer cannot create cars -> 403" "403" "$code"

echo "== uploads =="
# The upload must be a complete, decodable PNG: the server re-encodes jpg/png
# to drop metadata, so a bare header no longer passes.
png_fixture="bruno/pinolrent-api/fixtures/fit.png"
upload_json=$(curl -s -X POST "$BASE/uploads" -H "Authorization: Bearer $seller" -F "file=@$png_fixture;type=image/png")
upload_url=$(printf '%s' "$upload_json" | jq -r .url)
[ -n "$upload_url" ] && [ "$upload_url" != "null" ]; cond "upload image -> url ($upload_url)" $?
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE$upload_url")
check "download image -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/uploads" -H "Authorization: Bearer $seller" -F "file=@$0;type=text/x-shellscript")
check "upload non-image -> 415" "415" "$code"
# A bare PNG header: it sniffs as a png but declares no pixels to decode.
bad_png="$(mktemp /tmp/pinolrent-badpng.XXXXXX.png)"
printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde' > "$bad_png"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/uploads" -H "Authorization: Bearer $seller" -F "file=@$bad_png;type=image/png")
check "png without pixels -> 400" "400" "$code"
rm -f "$bad_png"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/uploads")
check "upload without token -> 401" "401" "$code"

car_json=$(curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
 -H 'Content-Type: application/json' \
 -d "{\"name\":\"Honda Fit\",\"price_per_day\":25000,\"photo_url\":\"$upload_url\"}")
car=$(printf '%s' "$car_json" | jq -r .id)
car_owner=$(printf '%s' "$car_json" | jq -r .owner_id)
check "create car" "number" "$([ "$car" != "null" ] && echo number)"
curl -s -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":true}' > /dev/null

echo "== reservations =="
res=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-01-10\",\"end_date\":\"2027-01-12\"}" | jq -r .id)
check "create reservation" "number" "$([ "$res" != "null" ] && echo number)"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-01-10\",\"end_date\":\"2027-01-12\"}")
check "overlap -> 409" "409" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2020-01-01\",\"end_date\":\"2020-01-02\"}")
check "past date -> 400" "400" "$code"

echo "== payments =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations/$res/payment" \
  -H "Authorization: Bearer $buyer" -H 'Content-Type: application/json' \
  -d '{"method":"pos","proof_url":"https://example.com/receipt.jpg"}')
check "record payment -> 201" "201" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/seller/reservations/$res/confirm" \
  -H "Authorization: Bearer $seller")
check "confirm -> 200" "200" "$code"

echo "== cancellation =="
res2=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-02-01\",\"end_date\":\"2027-02-03\"}" | jq -r .id)
check "reservation to cancel" "number" "$([ "$res2" != "null" ] && echo number)"
status=$(curl -s -X PATCH "$BASE/reservations/$res2/cancel" -H "Authorization: Bearer $buyer" | jq -r .status)
check "cancel -> cancelled" "cancelled" "$status"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/reservations/$res2/cancel" -H "Authorization: Bearer $buyer")
check "re-cancel -> 409" "409" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-02-01\",\"end_date\":\"2027-02-03\"}")
check "re-book released range -> 201" "201" "$code"

echo "== rejection =="
res3=$(curl -s -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-02-10\",\"end_date\":\"2027-02-12\"}" | jq -r .id)
check "reservation to reject" "number" "$([ "$res3" != "null" ] && echo number)"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations/$res3/payment" \
  -H "Authorization: Bearer $buyer" -H 'Content-Type: application/json' -d '{"method":"cash"}')
check "payment to reject -> 201" "201" "$code"
rej=$(curl -s -X PATCH "$BASE/seller/reservations/$res3/reject" -H "Authorization: Bearer $seller")
check "reject -> cancelled" "cancelled" "$(printf '%s' "$rej" | jq -r .status)"
check "payment rejected" "rejected" "$(printf '%s' "$rej" | jq -r .payment.status)"
n=$(curl -s "$BASE/cars?start_date=2027-02-11&end_date=2027-02-11" | jq --argjson id "$car" '[.[] | select(.id == $id)] | length')
check "dates released after rejection" "1" "$n"

echo "== day limit =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/reservations" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' \
  -d "{\"car_id\":$car,\"start_date\":\"2027-03-01\",\"end_date\":\"2027-04-05\"}")
check "range > 30 days -> 400" "400" "$code"

echo "== catalog: owner and pagination =="
n=$(curl -s "$BASE/cars?owner_id=$car_owner" | jq 'length')
check "owner_id filter" "1" "$n"
n=$(curl -s "$BASE/cars?owner_id=999999" | jq 'length')
check "nonexistent owner -> empty" "0" "$n"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars?owner_id=abc")
check "invalid owner_id -> 400" "400" "$code"
n=$(curl -s "$BASE/cars?limit=1" | jq 'length')
check "limit=1" "1" "$n"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars?limit=0")
check "invalid limit -> 400" "400" "$code"

echo "== car detail =="
res=$(curl -s "$BASE/cars/$car")
check "car detail -> id" "$car" "$(printf '%s' "$res" | jq -r .id)"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/999999")
check "nonexistent detail -> 404" "404" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/abc")
check "invalid detail id -> 400" "400" "$code"

echo "== seller contact =="
wa=$(curl -s "$BASE/cars/$car/contact" -H "Authorization: Bearer $buyer" | jq -r .whatsapp_url)
echo "$wa" | grep -q '^https://wa.me/56912345678'; cond "contact -> normalized wa.me link" $?
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/$car/contact")
check "contact without token -> 401" "401" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/999999/contact" -H "Authorization: Bearer $buyer")
check "contact for nonexistent car -> 404" "404" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":false}')
check "deactivate with future reservations -> 409" "409" "$code"
car2_json=$(curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"Inactive Car","price_per_day":10000}')
car2=$(printf '%s' "$car2_json" | jq -r .id)
curl -s -o /dev/null -X PATCH "$BASE/seller/cars/$car2" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"active":false}'
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/cars/$car2")
check "inactive detail -> 404" "404" "$code"

echo "== car edit and delete =="
price=$(curl -s -X PATCH "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"Honda Fit LX","price_per_day":30000}' | jq -r .price_per_day)
check "edit car -> price" "30000" "$price"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/seller/cars/$car" \
  -H "Authorization: Bearer $seller" -H 'Content-Type: application/json' -d '{}')
check "edit without fields -> 400" "400" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/seller/cars/$car" -H "Authorization: Bearer $seller")
check "delete car with reservations -> 409" "409" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/seller/cars/$car2" -H "Authorization: Bearer $seller")
check "delete car without reservations -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/seller/cars/$car2" -H "Authorization: Bearer $seller")
check "delete twice -> 404" "404" "$code"

echo "== /auth/me =="
me=$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $seller")
check "auth/me roles seller" "buyer,seller" "$(printf '%s' "$me" | jq -r '.roles | join(",")')"
check "auth/me email" "seller@example.com" "$(printf '%s' "$me" | jq -r .email)"

echo "== become-seller =="
upgraded=$(curl -s -X POST "$BASE/auth/become-seller" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' -d '{"phone":"+56987654321"}')
check "become-seller -> buyer,seller" "buyer,seller" "$(printf '%s' "$upgraded" | jq -r '.roles | join(",")')"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/become-seller" -H "Authorization: Bearer $buyer" \
  -H 'Content-Type: application/json' -d '{"phone":"bad"}')
check "become-seller invalid phone -> 400" "400" "$code"

echo "== hardening rules =="
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"X","price_per_day":1,"photo_url":"mailto:a@b.c"}')
check "photo_url mailto -> 400" "400" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/seller/cars" -H "Authorization: Bearer $seller" \
  -H 'Content-Type: application/json' -d '{"name":"X","price_per_day":100000001}')
check "price over cap -> 400" "400" "$code"

pad=$(head -c 1048600 /dev/zero | tr '\0' 'a')
{ printf '{"email":"'; printf '%s' "$pad"; printf '@a.io","password":"secret123"}'; } > /tmp/.pinolrent-big.json
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' --data-binary @/tmp/.pinolrent-big.json)
rm -f /tmp/.pinolrent-big.json
check "body > 1MB -> 413" "413" "$code"

echo "== password change =="
# token_valid_after has second precision: crossing the boundary guarantees the
# old token (issued in the previous second) lands strictly before the stamp.
sleep 1.1
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/auth/password" \
  -H "Authorization: Bearer $seller" -H 'Content-Type: application/json' \
  -d '{"current_password":"secret123","new_password":"newSecret456"}')
check "change password -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/auth/me" -H "Authorization: Bearer $seller")
check "old token after change -> 401" "401" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' -d '{"email":"seller@example.com","password":"secret123"}')
check "login with old password -> 401" "401" "$code"
seller=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"newSecret456"}' | jq -r .token)
[ -n "$seller" ] && [ "$seller" != "null" ]; cond "login with new password" $?

echo "== admin =="
# The admin role only exists when ADMIN_EMAILS names the account, so the
# smoke boots the server with the allow-list set to prove the whole path:
# registration grants the role, and /admin is reachable with that token.
adminEmail="admin@example.com"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$adminEmail\",\"password\":\"secret123\"}")
check "register admin -> 201" "201" "$code"

adminToken=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$adminEmail\",\"password\":\"secret123\"}" | jq -r .token)
[ -n "$adminToken" ] && [ "$adminToken" != "null" ]; cond "login admin" $?

roles=$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $adminToken" | jq -r '.roles | join(",")')
check "admin roles" "admin,buyer" "$roles"

# A plain buyer must not reach any admin route.
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/admin/users" -H "Authorization: Bearer $buyer")
check "admin route with buyer token -> 403" "403" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/admin/users")
check "admin route without token -> 401" "401" "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/admin/users" -H "Authorization: Bearer $adminToken")
check "admin route with admin token -> 200" "200" "$code"

# Suspension cuts the buyer's still-valid token and lifting it restores access
# without a new login.
buyerID=$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $buyer" | jq -r .id)
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$buyerID" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"suspended":true}')
check "suspend buyer -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/auth/me" -H "Authorization: Bearer $buyer")
check "suspended buyer -> 403" "403" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$buyerID" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"suspended":false}')
check "reactivate buyer -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/auth/me" -H "Authorization: Bearer $buyer")
check "reactivated buyer -> 200" "200" "$code"

# An administrator cannot lock themselves out.
adminID=$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $adminToken" | jq -r .id)
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$adminID" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"suspended":true}')
check "admin cannot suspend itself -> 400" "400" "$code"

# The allow-list is the only source of the role, so the API must refuse it.
# Revoke first: the flow above already turned the buyer into a seller, and a
# grant that changes nothing is deliberately left out of the audit log.
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$buyerID/roles" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"seller":false}')
check "revoke seller -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$buyerID/roles" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"seller":true}')
check "grant seller -> 200" "200" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$BASE/admin/users/$buyerID/roles" \
  -H "Authorization: Bearer $adminToken" -H 'Content-Type: application/json' \
  -d '{"admin":true}')
check "grant admin via API -> 400" "400" "$code"

# Read-only surfaces and the audit trail.
for path in admin/cars admin/reservations admin/payments admin/stats; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/$path" -H "Authorization: Bearer $adminToken")
  check "GET /$path -> 200" "200" "$code"
done
# The suspension pair above always writes two rows. Compared inside a
# substitution so a regression reports FAIL instead of tripping `set -e`.
audits=$(curl -s "$BASE/admin/audit" -H "Authorization: Bearer $adminToken" | jq -r '.total')
check "audit with 4+ entries" "yes" "$([ "${audits:-0}" -ge 4 ] && echo yes || echo no)"

echo "== rate limit /auth/* =="
blocked=0
for _ in $(seq 1 35); do
  c=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/login" \
    -H 'Content-Type: application/json' -d '{"email":"x@x.com","password":"y"}')
  [ "$c" = "429" ] && blocked=$((blocked + 1))
done
[ "$blocked" -gt 0 ]; cond "rate limit blocks (429 x $blocked)" $?

echo "== graceful shutdown =="
kill -TERM "$PID"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  kill -0 "$PID" 2>/dev/null || break
  sleep 0.2
done
if kill -0 "$PID" 2>/dev/null; then
  check "stops on SIGTERM" "yes" "no"
else
  check "stops on SIGTERM" "yes" "yes"
fi
PID=""

echo
echo "PASS: $pass  FAIL: $failures"
[ "$failures" -eq 0 ] || exit 1