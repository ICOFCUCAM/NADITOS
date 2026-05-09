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

# /v1/admin/users requires either ADMIN_BOOTSTRAP_KEY (the env var must
# also be set on the auth service) or an admin JWT. The first run has no
# admin to log in as, so the bootstrap key is the only option.
if [ -z "${ADMIN_BOOTSTRAP_KEY:-}" ]; then
  echo "ERROR: ADMIN_BOOTSTRAP_KEY env var must be set to seed users." >&2
  echo "       Set the same value on the auth service first, e.g." >&2
  echo "         openssl rand -hex 32" >&2
  echo "         fly secrets set -a naditos-auth ADMIN_BOOTSTRAP_KEY=<value>" >&2
  echo "       then run this script with the same value:" >&2
  echo "         ADMIN_BOOTSTRAP_KEY=<value> bash scripts/seed.sh" >&2
  exit 1
fi

echo "→ creating demo users"
for u in admin officer citizen; do
  curl_json -X POST "$API/v1/admin/users" \
    -H "X-Admin-Bootstrap-Key: $ADMIN_BOOTSTRAP_KEY" \
    -d "{
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
