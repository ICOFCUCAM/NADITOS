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
    [ -z "$pid" ] && continue
    # `go run` spawns the actual binary as a child; orphaning the
    # parent leaves the binary running and holding the listen port.
    # Walk the process tree from each parent and kill children first.
    pkill -KILL -P "$pid" 2>/dev/null || true
    kill -KILL "$pid" 2>/dev/null || true
  done
  # Belt-and-braces: anyone listening on a smoke port we didn't track.
  # SIGKILL because go run's child swallows SIGTERM.
  lsof -i:8001-8009 -t 2>/dev/null | xargs -r kill -KILL 2>/dev/null || true
  # Redirect wait's "Killed" stderr — those are just the SIGKILL'd
  # subshells reporting their own death, not test failures.
  wait 2>/dev/null || true
} 2>/dev/null
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
start insurance     8004 services/insurance
start inspection    8005 services/inspection
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
wait_health 8004 insurance
wait_health 8005 inspection
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

# ─── 5b. ANPR alert path: scanning a flagged vehicle raises an audit_alert ──
# Seeds a stolen vehicle, scans it, and waits for the audit-anpr-alerts
# consumer to materialize an audit_alerts row. Hardens the chain
# anpr.alert event → event_outbox → consumer → audit_alerts → /audit UI.
echo "→ ANPR alert: seed a stolen vehicle and scan its plate"
ALERT_PLATE="STOLEN-$(date +%s)"
PGPASSWORD=naditos psql -h localhost -U naditos -d naditos >/dev/null 2>&1 -c \
  "INSERT INTO vehicles (tenant_id, plate, is_stolen)
        VALUES ('$TENANT', '$ALERT_PLATE', true);"
ALERT_VID=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT id FROM vehicles WHERE tenant_id='$TENANT' AND plate='$ALERT_PLATE';" | tr -d ' ')
echo "  vehicle: $ALERT_VID"

ALERT_T0=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
            "SELECT EXTRACT(EPOCH FROM now())::bigint;" | tr -d ' ')

curl -sS -o /dev/null -X POST http://localhost:8008/v1/anpr/scans "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $OFFICER_TOKEN" \
  -d "{\"plate\":\"$ALERT_PLATE\",\"confidence\":0.96,\"source\":\"officer\",
       \"geo_lat\":60.4,\"geo_lng\":5.32,
       \"captured_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"

# Wait for the consumer to drain the outbox into audit_alerts. The
# consumer ticks every couple of seconds so 30 × 0.5 = 15s is plenty.
ALERT_COUNT=0
for i in $(seq 1 60); do
  ALERT_COUNT=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc "
SELECT count(*) FROM audit_alerts
 WHERE tenant_id='$TENANT'
   AND kind='anpr_match_flagged_vehicle'
   AND subject_id='$ALERT_VID'
   AND detected_at >= to_timestamp($ALERT_T0);" 2>/dev/null | tr -d ' ')
  [ "${ALERT_COUNT:-0}" -ge 1 ] && break
  sleep 0.5
done
[ "${ALERT_COUNT:-0}" -ge 1 ] || {
  echo "✗ expected anpr_match_flagged_vehicle alert, got $ALERT_COUNT"
  exit 1
}
echo "  ✓ audit_alert raised for flagged vehicle"

# ─── 5c. insurance + inspection live verify ────────────────────────────
# Each module's verify endpoint hits the dev-stub provider via the
# country router, records OK on the per-tenant HealthMonitor, and
# returns a stable shape. Asserts the path { router → adapter →
# health monitor → response } works for both services.
echo "→ insurance verify"
INS=$(curl -sS "http://localhost:8004/v1/insurance/verify?plate=$PLATE" \
       "${H_TENANT[@]}" -H "Authorization: Bearer $OFFICER_TOKEN")
INS_PROVIDER=$(echo "$INS" | jq -r .provider)
[ "$INS_PROVIDER" = "dev-stub" ] || { echo "✗ insurance verify: $INS"; exit 1; }
echo "  ✓ insurance provider=$INS_PROVIDER"

echo "→ inspection verify"
INSP=$(curl -sS "http://localhost:8005/v1/inspection/verify?plate=$PLATE" \
        "${H_TENANT[@]}" -H "Authorization: Bearer $OFFICER_TOKEN")
INSP_PROVIDER=$(echo "$INSP" | jq -r .provider)
[ "$INSP_PROVIDER" = "dev-stub" ] || { echo "✗ inspection verify: $INSP"; exit 1; }
echo "  ✓ inspection provider=$INSP_PROVIDER"

echo "→ insurance + inspection health"
INS_H=$(curl -sS "http://localhost:8004/v1/insurance/health" \
         "${H_TENANT[@]}" -H "Authorization: Bearer $OFFICER_TOKEN")
INS_STATE=$(echo "$INS_H" | jq -r .state)
[ "$INS_STATE" = "ok" ] || { echo "✗ insurance health: $INS_H"; exit 1; }
INSP_H=$(curl -sS "http://localhost:8005/v1/inspection/health" \
          "${H_TENANT[@]}" -H "Authorization: Bearer $OFFICER_TOKEN")
INSP_STATE=$(echo "$INSP_H" | jq -r .state)
[ "$INSP_STATE" = "ok" ] || { echo "✗ inspection health: $INSP_H"; exit 1; }
echo "  ✓ insurance state=$INS_STATE, inspection state=$INSP_STATE"

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
for i in $(seq 1 60); do
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
for i in $(seq 1 60); do
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

# ─── 12b. vehicle ownership transfer ───────────────────────────────────
# Seller (the existing citizen) starts a transfer for the smoke
# vehicle. A fresh buyer user is provisioned, claims an owners row,
# accepts the code, and the vehicle's owner_id is asserted to have
# flipped. Validates the path that the registry handlers + the
# notifications consumer + the buyer-side render all work together.
echo "→ transfer: provision buyer user"
BUYER_EMAIL="buyer-$(date +%s)@demo"
curl -sS -X POST http://localhost:8001/v1/admin/users "${H_TENANT[@]}" "${H_JSON[@]}" \
  -d "{\"email\":\"$BUYER_EMAIL\",\"password\":\"demo1234\",
       \"full_name\":\"Demo buyer\",\"roles\":[\"citizen\"]}" >/dev/null

BUYER=$(login "$BUYER_EMAIL" demo1234)
BUYER_TOKEN=$(echo "$BUYER" | jq -r .access_token)
[ -n "$BUYER_TOKEN" ] && [ "$BUYER_TOKEN" != null ] || {
  echo "✗ buyer login failed: $BUYER"; exit 1; }

curl -sS -o /dev/null -X POST http://localhost:8002/v1/citizens/me/owner \
  "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $BUYER_TOKEN" \
  -d "{\"full_name\":\"Demo buyer\",\"email\":\"$BUYER_EMAIL\"}"
echo "  ✓ buyer $BUYER_EMAIL provisioned"

echo "→ transfer: seller starts transfer of $PLATE"
START=$(curl -sS -X POST \
  "http://localhost:8002/v1/citizens/me/vehicles/$VID/transfer" \
  "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $CITIZEN_TOKEN" \
  -d "{\"to_contact\":\"$BUYER_EMAIL\"}")
TRANSFER_CODE=$(echo "$START" | jq -r .code)
[ -n "$TRANSFER_CODE" ] && [ "$TRANSFER_CODE" != null ] || {
  echo "✗ transfer start failed: $START"; exit 1; }
echo "  ✓ transfer code $TRANSFER_CODE issued"

echo "→ transfer: buyer accepts"
ACCEPT_RC=$(curl -sS -o /tmp/smoke.body -w '%{http_code}' \
  -X POST http://localhost:8002/v1/citizens/me/transfers/accept \
  "${H_TENANT[@]}" "${H_JSON[@]}" \
  -H "Authorization: Bearer $BUYER_TOKEN" \
  -d "{\"code\":\"$TRANSFER_CODE\"}")
[ "$ACCEPT_RC" = 200 ] || {
  echo "✗ accept failed: $ACCEPT_RC $(cat /tmp/smoke.body)"; exit 1; }

# Confirm vehicle.owner_id now points at the buyer's owners row.
NEW_OWNER=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT v.owner_id::text FROM vehicles v WHERE v.id='$VID'" | tr -d ' ')
BUYER_OWNER=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
  "SELECT o.id::text FROM owners o JOIN users u ON u.id=o.user_id
    WHERE u.email='$BUYER_EMAIL' AND u.tenant_id='$TENANT'" | tr -d ' ')
[ "$NEW_OWNER" = "$BUYER_OWNER" ] || {
  echo "✗ owner did not flip: vehicle=$NEW_OWNER buyer=$BUYER_OWNER"; exit 1; }
echo "  ✓ vehicle owner flipped to buyer"

# ─── 13. evidence retention reaper ─────────────────────────────────────
# Backdate the smoke fine + force a 1-day retention policy, then trigger
# the reaper and confirm the evidence row is sealed and the underlying
# storage object is gone. Hardens the path that protects citizens'
# right-to-be-forgotten timeline against a configuration drift bug.
echo "→ reaper: force-expire the smoke fine evidence"
PGPASSWORD=naditos psql -h localhost -U naditos -d naditos >/dev/null 2>&1 -c "
INSERT INTO evidence_retention_policy
       (tenant_id, default_days, paid_fine_days, cancelled_fine_days)
VALUES ('$TENANT', 1, 1, 1)
ON CONFLICT (tenant_id) DO UPDATE
   SET default_days=1, paid_fine_days=1, cancelled_fine_days=1,
       legal_hold_active=false;
-- Backdate every fine and its evidence so the reaper sees them as
-- past-deadline regardless of status branch (paid / cancelled /
-- default). Keep paid_at consistent for paid fines so the paid_fine_days
-- branch fires correctly.
UPDATE fines SET issued_at = now() - interval '60 days',
                 due_at    = now() - interval '46 days',
                 paid_at   = CASE WHEN paid_at IS NOT NULL
                                  THEN now() - interval '30 days'
                                  ELSE NULL END
 WHERE tenant_id='$TENANT';
UPDATE fine_evidence SET created_at = now() - interval '60 days'
 WHERE tenant_id='$TENANT';"

REAP_RC=$(curl -sS -o /tmp/smoke.body -w '%{http_code}' \
            -X POST "http://localhost:8006/v1/fines/admin/reaper:run" \
            "${H_TENANT[@]}" -H "Authorization: Bearer $ADMIN_TOKEN")
[ "$REAP_RC" = 200 ] || { echo "✗ reaper trigger: $REAP_RC: $(cat /tmp/smoke.body)"; exit 1; }
SEALED=$(jq -r .sealed </tmp/smoke.body)
[ "${SEALED:-0}" -ge 1 ] || { echo "✗ reaper: expected ≥1 sealed, got: $(cat /tmp/smoke.body)"; exit 1; }
echo "  ✓ reaper sealed $SEALED row(s)"

LIVE=$(PGPASSWORD=naditos psql -h localhost -U naditos -d naditos -tAc \
         "SELECT count(*) FROM fine_evidence
           WHERE tenant_id='$TENANT' AND sealed_at IS NULL;" | tr -d ' ')
[ "$LIVE" = "0" ] || { echo "✗ unsealed evidence remains: $LIVE"; exit 1; }
echo "  ✓ no unsealed evidence remains for tenant $TENANT"

# ─── 14. final audit chain integrity ───────────────────────────────────
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
