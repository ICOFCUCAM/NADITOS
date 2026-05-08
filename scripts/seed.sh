#!/usr/bin/env bash
# Seed demo users + a few vehicles by hitting the auth + registry services.
# Requires the stack running (`make up`).
set -euo pipefail

API="${API:-http://localhost:8001}"
REG="${REG:-http://localhost:8002}"
TENANT="${TENANT:-demo}"

curl_json() {
  curl -sS -H 'Content-Type: application/json' -H "X-Tenant-Id: $TENANT" "$@"
}

echo "→ creating demo users"
for u in admin officer citizen; do
  curl_json -X POST "$API/v1/admin/users" -d "{
    \"email\":\"${u}@demo\",
    \"password\":\"demo1234\",
    \"full_name\":\"Demo ${u}\",
    \"roles\":[\"${u}\"]
  }" || true
done

echo
echo "→ login as admin"
TOKEN=$(curl_json -X POST "$API/v1/auth/login" \
  -d '{"email":"admin@demo","password":"demo1234"}' | jq -r .access_token)
echo "token: ${TOKEN:0:24}…"

echo "→ creating demo vehicles"
for plate in "AB-12-CD" "XY-99-ZZ" "STOLEN-1"; do
  curl_json -H "Authorization: Bearer $TOKEN" -X POST "$REG/v1/vehicles" \
    -d "{\"plate\":\"$plate\",\"make\":\"Demo\",\"model\":\"X\",\"year\":2020}" || true
done

echo
echo "Done. Try: curl -H 'Authorization: Bearer \$TOKEN' -H 'X-Tenant-Id: demo' $REG/v1/vehicles"
