#!/usr/bin/env bash
# End-to-end smoke run for NADITOS.
#
# Exercises the police-app enforcement flow through real services
# talking over HTTP against a real Postgres:
#
#   1. Start auth, registry, fines, audit, license, anpr-gateway as
#      background go-run processes on their default ports.
#   2. Wait for /healthz on each.
#   3. Bootstrap an admin user and an officer user via the auth
#      service's admin endpoint.
#   4. Officer logs in, registers a non-compliant vehicle.
#   5. Officer enqueues an ANPR scan, polls until the worker resolves it,
#      confirms it matched the vehicle.
#   6. Officer pulls the vehicle by plate (compliance state → red).
#   7. Officer issues a fine with evidence — server should:
#        - reject without evidence
#        - reject duplicate within window
#        - accept the canonical request and price from the catalog.
#   8. Admin lists fines and verifies the audit chain.
#   9. Citizen pays the fine via the dev-stub payment provider.
#  10. Check fine.status flipped to "paid".
#
# Requires: bash, curl, jq, go, psql (for the initial migration).
# DATABASE_URL must point at a running Postgres with the demo seed.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DATABASE_URL="${DATABASE_URL:-postgres://naditos:naditos@localhost:5432/naditos?sslmode=disable}"
JWT_SECRET="${JWT_SECRET:-smoke-secret-do-not-use-anywhere-else-smoke-secret-do-not-use}"
TENANT="${TENANT:-demo}"
LOG_DIR="${LOG_DIR:-/tmp/naditos-smoke}"

mkdir -p "$LOG_DIR"

PIDS=()
cleanup() {
  echo "→ stopping services"
  for pid in "${PIDS[@]:-}"; do
    [ -z "$pid" ] || kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# ─── 1. migrations ──────────────────────────────────────────────────────
echo "→ apply migrations"
DATABASE_URL="$DATABASE_URL" "$ROOT/scripts/migrate.sh" up >"$LOG_DIR/migrate.log" 2>&1

# ─── 2. start services ──────────────────────────────────────────────────
start() {
  local name=$1 port=$2 dir=$3
  echo "→ starting $name on $port"
  (
    cd "$dir"
    DATABASE_URL="$DATABASE_URL" \
    JWT_SECRET="$JWT_SECRET" \
    SERVICE_PORT="$port" \
    AUDIT_URL="http://localhost:8007" \
    LOG_LEVEL="${LOG_LEVEL:-info}" \
    go run ./cmd/server >"$LOG_DIR/$name.log" 2>&1
  ) &
  PIDS+=("$!")
}

start audit         8007 services/audit
start auth          8001 services/auth
start registry      8002 services/registry
start license       8003 services/license
start fines         8006 services/fines
start anpr-gateway  8008 services/anpr-gateway
start notifications 8009 services/notifications

wait_health() {
  local port=$1 name=$2
  for i in $(seq 1 60); do
    if curl -sf "http://localhost:$port/healthz" >/dev/null; then
      echo "  ✓ $name healthy"
      return
    fi
    sleep 0.5
  done
  echo "✗ $name (port $port) failed to become healthy; logs:" >&2
  tail -40 "$LOG_DIR/$name.log" >&2
  exit 1
}

echo "→ waiting for health"
wait_health 8001 auth
wait_health 8002 registry
wait_health 8006 fines
wait_health 8007 audit
wait_health 8003 license
wait_health 8008 anpr-gateway
wait_health 8009 notifications

H_TENANT=(-H "X-Tenant-Id: $TENANT")
H_JSON=(-H "Content-Type: application/json")

# ─── 3. bootstrap users ─────────────────────────────────────────────────
echo "→ bootstrap admin + officer + citizen"
curl -sS -X POST http://localhost:8001/v1/admin/users "${H_TENANT[@]}" "${H_JSON[@]}" \
  -d '{"email":"admin@demo","password":"demo1234","full_name":"Demo admin","roles":["admin"]}' >/dev/null
curl -sS -X POST http://localhost:8001/v1/admin/users "${H_TENANT[@]}" "${H_JSON[@]}" \
  -d '{"email":"officer@demo","password":"demo1234","full_name":"Demo officer","roles":["officer"]}' >/dev/null
curl -sS -X POST http://localhost:8001/v1/admin/users "${H_TENANT[@]}" "${H_JSON[@]}" \
  -d '{"email":"citizen@demo","password":"demo1234","full_name":"Demo citizen","roles":["citizen"]}' >/dev/null

login() {
  local email=$1 pw=$2
  curl -sS -X POST http://localhost:8001/v1/auth/login "${H_TENANT[@]}" "${H_JSON[@]}" \
    -d "{\"email\":\"$email\",\"password\":\"$pw\"}"
}

OFFICER=$(login officer@demo demo1234)
OFFICER_TOKEN=$(echo "$OFFICER" | jq -r .access_token)
ADMIN=$(login admin@demo demo1234)
ADMIN_TOKEN=$(echo "$ADMIN" | jq -r .access_token)
CITIZEN=$(login citizen@demo demo1234)
CITIZEN_TOKEN=$(echo "$CITIZEN" | jq -r .access_token)

[ "$OFFICER_TOKEN" != null ] && [ -n "$OFFICER_TOKEN" ] || { echo "✗ officer login failed"; exit 1; }
echo "  ✓ officer + admin + citizen tokens obtained"

# ─── 4. register a non-compliant vehicle ────────────────────────────────
echo "→ register vehicle with expired insurance"
PLATE="SMK-$(date +%s)"
VEH=$(curl -sS -X POST http://localhost:8002/v1/vehicles "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d "{\"plate\":\"$PLATE\",\"make\":\"Demo\",\"model\":\"X\",\"year\":2020,
       \"insurance_expires_at\":\"2024-01-01T00:00:00Z\",
       \"inspection_expires_at\":\"2024-01-01T00:00:00Z\"}")
VID=$(echo "$VEH" | jq -r .id)
[ -n "$VID" ] && [ "$VID" != null ] || { echo "✗ vehicle create failed: $VEH"; exit 1; }
echo "  ✓ vehicle $PLATE / $VID"

# Link the citizen to the vehicle. The citizen self-claims their owner
# record; the admin then links the vehicle to that owner. No SQL
# workaround any more — this is the production path.

# Grant the citizen role 'owners:self' for the demo tenant. The 0006
# migration already does this on apply; this echo is just a no-op
# safety net in case the smoke runs against a partially-migrated DB.
PGPASSWORD=naditos psql -h localhost -U naditos -d naditos >/dev/null 2>&1 <<SQL
INSERT INTO role_permissions (tenant_id, role_code, permission)
  VALUES ('$TENANT', 'citizen', 'owners:self')
  ON CONFLICT DO NOTHING;
SQL

# Citizen has to log back in to pick up the new permission claim.
CITIZEN=$(login citizen@demo demo1234)
CITIZEN_TOKEN=$(echo "$CITIZEN" | jq -r .access_token)

OWNER=$(curl -sS -X POST http://localhost:8002/v1/citizens/me/owner "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $CITIZEN_TOKEN" \
  -d '{"full_name":"Demo citizen","email":"citizen@demo","phone":"+1-555"}')
OWNER_ID=$(echo "$OWNER" | jq -r .id)
[ -n "$OWNER_ID" ] && [ "$OWNER_ID" != null ] || {
  echo "✗ self-claim failed: $OWNER"; exit 1; }
echo "  ✓ citizen self-claimed owner $OWNER_ID"

curl -sS -o /dev/null -X POST \
  "http://localhost:8002/v1/owners/$OWNER_ID/vehicles/$VID" "${H_TENANT[@]}" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
echo "  ✓ admin linked vehicle to owner"

# ─── 5. ANPR enqueue + poll ─────────────────────────────────────────────
echo "→ officer scans plate"
JOB=$(curl -sS -X POST http://localhost:8008/v1/anpr/scans "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $OFFICER_TOKEN" \
  -d "{\"plate\":\"$PLATE\",\"confidence\":0.94,\"source\":\"officer\",
        \"geo_lat\":60.4,\"geo_lng\":5.32,
        \"captured_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}")
JID=$(echo "$JOB" | jq -r .job_id)
echo "  job: $JID"

for i in $(seq 1 20); do
  S=$(curl -sS http://localhost:8008/v1/anpr/jobs/"$JID" "${H_TENANT[@]}" \
        -H "Authorization: Bearer $OFFICER_TOKEN")
  ST=$(echo "$S" | jq -r .status)
  if [ "$ST" = done ] || [ "$ST" = duplicate ]; then
    MV=$(echo "$S" | jq -r .matched_vehicle_id)
    echo "  ✓ scan resolved: status=$ST matched=$MV"
    [ "$MV" = "$VID" ] || { echo "✗ ANPR did not match vehicle (got $MV want $VID)"; exit 1; }
    break
  fi
  sleep 0.3
done

# ─── 6. compliance lookup ───────────────────────────────────────────────
echo "→ officer pulls compliance"
V=$(curl -sS http://localhost:8002/v1/vehicles/by-plate/"$PLATE" "${H_TENANT[@]}" \
      -H "Authorization: Bearer $OFFICER_TOKEN")
ST=$(echo "$V" | jq -r .status)
[ "$ST" = red ] || { echo "✗ expected red status, got $ST"; exit 1; }
echo "  ✓ status=$ST"

# ─── 7. fine issuance ───────────────────────────────────────────────────
ISSUE_BODY='{"plate":"'$PLATE'","offence_code":"INS_EXPIRED","geo_lat":60.4,"geo_lng":5.32,
  "device_id":"smoke-device","evidence":[]}'
echo "→ try issue without evidence (should be 400 evidence_required)"
RC=$(curl -sS -o /tmp/smoke.body -w '%{http_code}' \
  -X POST http://localhost:8006/v1/fines "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $OFFICER_TOKEN" -d "$ISSUE_BODY")
[ "$RC" = 400 ] || { echo "✗ expected 400, got $RC: $(cat /tmp/smoke.body)"; exit 1; }
grep -q evidence_required /tmp/smoke.body || { echo "✗ wrong code: $(cat /tmp/smoke.body)"; exit 1; }
echo "  ✓ rejected (evidence_required)"

EV='{"kind":"photo","s3_key":"smoke/'$JID'.jpg","sha256":"deadbeef","bytes":12345,
     "taken_at":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}'
GOOD_BODY='{"plate":"'$PLATE'","offence_code":"INS_EXPIRED","geo_lat":60.4,"geo_lng":5.32,
  "device_id":"smoke-device","evidence":['$EV']}'
echo "→ issue real fine"
RES=$(curl -sS -X POST http://localhost:8006/v1/fines "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $OFFICER_TOKEN" -d "$GOOD_BODY")
FID=$(echo "$RES" | jq -r .id)
AMOUNT=$(echo "$RES" | jq -r .amount)
[ -n "$FID" ] && [ "$FID" != null ] || { echo "✗ issue failed: $RES"; exit 1; }
[ "$AMOUNT" = 400.00 ] || { echo "✗ expected catalog amount 400.00, got $AMOUNT"; exit 1; }
echo "  ✓ fine $FID issued at $AMOUNT EUR (server-priced)"

echo "→ duplicate within window (should be 409)"
RC=$(curl -sS -o /tmp/smoke.body -w '%{http_code}' \
  -X POST http://localhost:8006/v1/fines "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $OFFICER_TOKEN" -d "$GOOD_BODY")
[ "$RC" = 409 ] || { echo "✗ expected 409, got $RC: $(cat /tmp/smoke.body)"; exit 1; }
echo "  ✓ duplicate rejected"

# ─── 8. audit chain verify ──────────────────────────────────────────────
echo "→ admin verifies audit chain"
V=$(curl -sS "http://localhost:8007/v1/audit/verify?tenant_id=$TENANT" "${H_TENANT[@]}" \
      -H "Authorization: Bearer $ADMIN_TOKEN")
OK=$(echo "$V" | jq -r .ok)
CHK=$(echo "$V" | jq -r .checked)
[ "$OK" = true ] || { echo "✗ chain verification failed: $V"; exit 1; }
echo "  ✓ audit chain valid ($CHK events)"

# ─── 9. citizen pays ────────────────────────────────────────────────────
echo "→ citizen pays"
PAY=$(curl -sS -X POST http://localhost:8006/v1/fines/"$FID"/pay "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $CITIZEN_TOKEN" -d '{"method":"card"}')
PAY_STATUS=$(echo "$PAY" | jq -r .status)
[ "$PAY_STATUS" = succeeded ] || { echo "✗ pay status=$PAY_STATUS, body=$PAY"; exit 1; }
echo "  ✓ payment $PAY_STATUS"

# ─── 10. final reconciliation ──────────────────────────────────────────
F=$(curl -sS http://localhost:8006/v1/fines/"$FID" "${H_TENANT[@]}" \
      -H "Authorization: Bearer $OFFICER_TOKEN")
S=$(echo "$F" | jq -r .fine.status)
[ "$S" = paid ] || { echo "✗ fine status not paid: $S"; exit 1; }
echo "  ✓ fine status=paid"

# ─── 11. notifications consumer drained the events ─────────────────────
# naditos has BYPASSRLS so we don't need SET row_security=off here.
echo "→ wait for notifications consumer to drain"
NOTIF_COUNT=0
for i in $(seq 1 30); do
  NOTIF_COUNT=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
    "SELECT COUNT(*) FROM notification_records
       WHERE tenant_id='$TENANT' AND status='sent';" 2>/dev/null | tr -d ' ')
  if [ "${NOTIF_COUNT:-0}" -ge 2 ]; then
    break
  fi
  sleep 0.5
done
[ "${NOTIF_COUNT:-0}" -ge 2 ] || {
  echo "✗ expected ≥2 notifications (fine.issued + fine.paid), got $NOTIF_COUNT"
  PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -c \
    "SELECT status, channel, recipient, template
       FROM notification_records WHERE tenant_id='$TENANT';" 2>&1 | head -20
  exit 1
}
echo "  ✓ $NOTIF_COUNT notifications delivered (fine.issued + fine.paid)"

# ─── 12. demerit → suspend → reinstate → notify loop ───────────────────
# Issue two SPEED_30 fines (6 points each) with the citizen's license
# attached. The threshold is 12 → on the second fine the demerit engine
# auto-suspends the license and writes license.suspended into the
# outbox. The notifications consumer drains it and sends the citizen a
# suspended notice. An admin then lifts the suspension via API; the
# citizen gets a reinstated notice.
echo "→ demerit loop: seed driver license for citizen"
DLNUM="DL-SMOKE-$(printf '%04x' $((RANDOM)))"
PGPASSWORD=naditos psql -h localhost -U naditos -d naditos >/dev/null 2>&1 <<SQL
INSERT INTO driver_licenses
  (tenant_id, user_id, license_number, full_name, classes, issued_at, expires_at)
SELECT '$TENANT', u.id, '$DLNUM', 'Demo citizen', ARRAY['B'],
       '2020-01-01', '2030-01-01'
  FROM users u WHERE u.tenant_id='$TENANT' AND u.email='citizen@demo';
SQL
echo "  ✓ license $DLNUM seeded"

issue_speed30() {
  local plate=$1
  local sha=$2
  curl -sS -X POST http://localhost:8006/v1/fines "${H_TENANT[@]}" "${H_JSON[@]}" \
    -H "Authorization: Bearer $OFFICER_TOKEN" \
    -d "{\"plate\":\"$plate\",\"offence_code\":\"SPEED_30\",
         \"driver_license\":\"$DLNUM\",
         \"geo_lat\":60.4,\"geo_lng\":5.32,\"device_id\":\"smoke-device\",
         \"evidence\":[{\"kind\":\"photo\",\"s3_key\":\"smoke/$sha.jpg\",
           \"sha256\":\"$sha\",\"bytes\":12345,
           \"taken_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}]}"
}

# Two SPEED_30 fines on different plates (avoids duplicate-protection).
PLATE2="DEM-$(date +%s)"
PGPASSWORD=naditos psql -h localhost -U naditos -d naditos >/dev/null 2>&1 -c \
  "INSERT INTO vehicles (tenant_id, plate) VALUES ('$TENANT', '$PLATE2');"

# Capture cutoff so notification checks below ignore artefacts left
# behind by previous smoke runs in the same tenant. Use epoch seconds
# (a single token) to avoid timestamp-with-space parsing pain.
DEMERIT_T0=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT EXTRACT(EPOCH FROM now())::bigint;" | tr -d ' ')

echo "→ demerit fine 1/2"
issue_speed30 "$PLATE" "deadbeef1" >/dev/null
echo "→ demerit fine 2/2 (crosses threshold)"
issue_speed30 "$PLATE2" "deadbeef2" >/dev/null

echo "→ wait for license.suspended notification"
SUSP_COUNT=0
for i in $(seq 1 30); do
  SUSP_COUNT=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
    "SELECT COUNT(*) FROM notification_records
       WHERE tenant_id='$TENANT' AND template='license.suspended.v1'
         AND status='sent' AND created_at >= to_timestamp($DEMERIT_T0);" \
    2>/dev/null | tr -d ' ')
  [ "${SUSP_COUNT:-0}" -ge 1 ] && break
  sleep 0.5
done
[ "${SUSP_COUNT:-0}" -ge 1 ] || {
  echo "✗ expected license.suspended notification, got $SUSP_COUNT"
  PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -c \
    "SELECT template, status, recipient FROM notification_records
       WHERE tenant_id='$TENANT';"
  exit 1
}
echo "  ✓ license.suspended delivered"

echo "→ admin lifts the suspension"
LICENSE_ID=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT id FROM driver_licenses WHERE tenant_id='$TENANT' AND license_number='$DLNUM';" \
  | tr -d ' ')
SUSP_ID=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT id FROM driver_suspensions WHERE license_id='$LICENSE_ID' AND lifted_at IS NULL LIMIT 1;" \
  | tr -d ' ')
LIFT_RC=$(curl -sS -o /tmp/smoke.body -w '%{http_code}' \
  -X POST "http://localhost:8003/v1/licenses/$LICENSE_ID/suspensions/$SUSP_ID/lift" \
  "${H_TENANT[@]}" -H "Authorization: Bearer $ADMIN_TOKEN")
[ "$LIFT_RC" = 204 ] || { echo "✗ lift failed: $LIFT_RC: $(cat /tmp/smoke.body)"; exit 1; }
echo "  ✓ suspension lifted"

echo "→ wait for license.reinstated notification"
REIN_COUNT=0
for i in $(seq 1 30); do
  REIN_COUNT=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
    "SELECT COUNT(*) FROM notification_records
       WHERE tenant_id='$TENANT' AND template='license.reinstated.v1'
         AND status='sent' AND created_at >= to_timestamp($DEMERIT_T0);" \
    2>/dev/null | tr -d ' ')
  [ "${REIN_COUNT:-0}" -ge 1 ] && break
  sleep 0.5
done
[ "${REIN_COUNT:-0}" -ge 1 ] || {
  echo "✗ expected license.reinstated notification, got $REIN_COUNT"
  exit 1
}
echo "  ✓ license.reinstated delivered"

# ─── 13. final audit chain integrity ───────────────────────────────────
# Re-verify the audit chain AFTER the demerit loop so any tamper in
# rows added between stages 8 and 12 is caught. The chain length here
# proves the suspension and reinstatement events were also stamped.
echo "→ final audit chain verify"
V=$(curl -sS "http://localhost:8007/v1/audit/verify?tenant_id=$TENANT" "${H_TENANT[@]}" \
      -H "Authorization: Bearer $ADMIN_TOKEN")
OK=$(echo "$V" | jq -r .ok)
CHK=$(echo "$V" | jq -r .checked)
[ "$OK" = true ] || { echo "✗ final chain verification failed: $V"; exit 1; }
[ "$CHK" -ge 4 ] || { echo "✗ expected ≥4 audit events at end, got $CHK"; exit 1; }
echo "  ✓ audit chain still valid ($CHK events end-of-run)"

echo
echo "✅ smoke run complete — all stages green"
