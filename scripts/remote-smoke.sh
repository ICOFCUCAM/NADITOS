#!/usr/bin/env bash
# remote-smoke.sh — exercise the live gateway end-to-end.
#
# This is the deployment-side counterpart to scripts/smoke.sh (which
# spawns local go-run processes). Use this against a real Fly stack
# to verify that the deployed services are actually reachable and that
# the most-used paths return what the apps expect.
#
# Usage:
#   API=https://naditos-gateway.fly.dev TENANT=demo bash scripts/remote-smoke.sh
#   make remote-smoke
#
# Environment:
#   API           — gateway base URL (default https://naditos-gateway.fly.dev)
#   TENANT        — tenant id          (default demo)
#   ADMIN_EMAIL   — admin login        (default admin@demo)
#   OFFICER_EMAIL — officer login      (default officer@demo)
#   PASSWORD      — shared password    (default demo1234)
#
# Exit code is 0 on full pass, non-zero on first failure. Each stage
# prints PASS/FAIL on its own line so the output is grep-friendly.

set -uo pipefail

API="${API:-https://naditos-gateway.fly.dev}"
TENANT="${TENANT:-demo}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@demo}"
OFFICER_EMAIL="${OFFICER_EMAIL:-officer@demo}"
PASSWORD="${PASSWORD:-demo1234}"

passes=0
fails=0
pass() { printf "  \033[32m✓\033[0m %s\n" "$1"; passes=$((passes+1)); }
fail() { printf "  \033[31m✗\033[0m %s\n  ↳ %s\n" "$1" "$2"; fails=$((fails+1)); }

curl_status() {
  # Echo HTTP status; body to /tmp/remote-smoke-body.
  curl -sS -o /tmp/remote-smoke-body -w '%{http_code}' "$@"
}

echo "▸ remote smoke against $API (tenant=$TENANT)"
echo

# ─── 1. gateway healthz ────────────────────────────────────────────────
code=$(curl_status "$API/healthz")
if [ "$code" = "200" ] && grep -q '"ok":true' /tmp/remote-smoke-body; then
  pass "gateway /healthz -> $code {\"ok\":true}"
else
  fail "gateway /healthz" "got $code: $(head -c 200 /tmp/remote-smoke-body)"
  echo "  (giving up; if the gateway isn't healthy nothing else is)"
  exit 1
fi

# ─── 2. login as admin ─────────────────────────────────────────────────
code=$(curl_status -X POST "$API/v1/auth/login" \
  -H 'Content-Type: application/json' -H "X-Tenant-Id: $TENANT" \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$PASSWORD\"}")
if [ "$code" = "200" ]; then
  ADMIN_TOK=$(jq -r .access_token < /tmp/remote-smoke-body)
  TENANT_REGEX=$(jq -r '.user.tenant_config.plate_regex // "?"' < /tmp/remote-smoke-body)
  TENANT_CCY=$(jq -r '.user.tenant_config.currency // "?"' < /tmp/remote-smoke-body)
  pass "admin login (tenant_config.currency=$TENANT_CCY, plate_regex=$TENANT_REGEX)"
else
  fail "admin login" "got $code: $(head -c 200 /tmp/remote-smoke-body)"
fi

# ─── 3. login as officer ───────────────────────────────────────────────
code=$(curl_status -X POST "$API/v1/auth/login" \
  -H 'Content-Type: application/json' -H "X-Tenant-Id: $TENANT" \
  -d "{\"email\":\"$OFFICER_EMAIL\",\"password\":\"$PASSWORD\"}")
if [ "$code" = "200" ]; then
  OFFICER_TOK=$(jq -r .access_token < /tmp/remote-smoke-body)
  pass "officer login"
else
  fail "officer login" "got $code: $(head -c 200 /tmp/remote-smoke-body)"
fi

# ─── 4. /v1/auth/me round-trips identity ───────────────────────────────
code=$(curl_status -H "Authorization: Bearer ${OFFICER_TOK:-x}" \
                   -H "X-Tenant-Id: $TENANT" "$API/v1/auth/me")
if [ "$code" = "200" ] && grep -q '"role":"officer"' /tmp/remote-smoke-body; then
  pass "/v1/auth/me as officer -> role=officer"
else
  fail "/v1/auth/me" "got $code: $(head -c 200 /tmp/remote-smoke-body)"
fi

# ─── 5. registry list (admin) ──────────────────────────────────────────
code=$(curl_status -H "Authorization: Bearer ${ADMIN_TOK:-x}" \
                   -H "X-Tenant-Id: $TENANT" "$API/v1/vehicles?limit=5")
if [ "$code" = "200" ]; then
  count=$(jq -r '.items | length' < /tmp/remote-smoke-body 2>/dev/null || echo "?")
  pass "GET /v1/vehicles -> $count items"
else
  fail "GET /v1/vehicles" "got $code"
fi

# ─── 6. plate-regex enforcement: bad plate ─────────────────────────────
code=$(curl_status -X POST "$API/v1/vehicles" \
  -H "Authorization: Bearer ${ADMIN_TOK:-x}" -H "X-Tenant-Id: $TENANT" \
  -H 'Content-Type: application/json' \
  -d '{"plate":"WAY-WAY-WAY-TOO-LONG-FOR-ANY-COUNTRY-13-CHARS"}')
body=$(head -c 200 /tmp/remote-smoke-body)
if [ "$code" = "400" ] && grep -q '"code":"bad_plate"' /tmp/remote-smoke-body; then
  pass "plate-regex rejects oversized plate (400 bad_plate)"
else
  fail "plate-regex enforcement" "got $code: $body"
fi

# ─── 7. audit verify (admin role) ──────────────────────────────────────
code=$(curl_status -H "Authorization: Bearer ${ADMIN_TOK:-x}" \
                   -H "X-Tenant-Id: $TENANT" "$API/v1/audit/verify?tenant_id=$TENANT")
if [ "$code" = "200" ] && grep -q '"ok":true' /tmp/remote-smoke-body; then
  checked=$(jq -r .checked < /tmp/remote-smoke-body 2>/dev/null || echo "?")
  pass "audit /verify -> ok=true checked=$checked"
elif [ "$code" = "200" ] && grep -q '"ok":false' /tmp/remote-smoke-body; then
  fail "audit chain verification" "BROKEN: $(head -c 200 /tmp/remote-smoke-body)"
else
  fail "audit /verify" "got $code"
fi

# ─── 8. citizen self-service (negative case is OK) ─────────────────────
code=$(curl_status -X POST "$API/v1/auth/login" \
  -H 'Content-Type: application/json' -H "X-Tenant-Id: $TENANT" \
  -d "{\"email\":\"citizen@$TENANT\",\"password\":\"$PASSWORD\"}")
if [ "$code" = "200" ]; then
  CITIZEN_TOK=$(jq -r .access_token < /tmp/remote-smoke-body)
  code=$(curl_status -H "Authorization: Bearer $CITIZEN_TOK" \
                     -H "X-Tenant-Id: $TENANT" "$API/v1/citizens/me/vehicles")
  if [ "$code" = "200" ]; then
    pass "citizen /v1/citizens/me/vehicles -> 200"
  else
    fail "citizen self-service" "got $code on /me/vehicles"
  fi
else
  fail "citizen login" "got $code (citizen@$TENANT may not be seeded for this tenant)"
fi

echo
echo "▸ $passes passed, $fails failed"
[ "$fails" -eq 0 ] || exit 1
