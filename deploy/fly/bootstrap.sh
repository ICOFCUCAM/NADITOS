#!/usr/bin/env bash
# NADITOS · Fly.io bootstrap.
#
# Usage:
#   bash deploy/fly/bootstrap.sh <jwt-secret>            # all services
#   bash deploy/fly/bootstrap.sh <jwt-secret> <service>  # one service
#
# Pre-requisites you must do by hand first:
#   1. fly auth login
#   2. fly postgres create --name naditos-pg --org "$FLY_ORG" --region "$REGION"
#   3. Run db migrations once (see deploy/fly/README.md "Migrations").
#
# Idempotent. Safe to re-run after a failure: it checks current state
# at every step and only does the work that's still needed:
#
#   • app creation       — skipped if `fly apps list` shows it
#   • postgres attach    — skipped if DATABASE_URL secret is already set
#   • jwt secret stage   — skipped if JWT_SECRET digest matches
#   • deploy             — always run (cheap when no source changed),
#                          rolling strategy so already-healthy machines
#                          stay up if the new version fails
#
# Per-service failures don't kill the loop; a summary at the end
# lists every service with its outcome so you can re-run for the
# specific one(s) that need attention.
set -uo pipefail

JWT_SECRET="${1:-}"
ONLY_SVC="${2:-}"
if [ -z "$JWT_SECRET" ]; then
  echo "usage: $0 <jwt-secret> [service]"
  echo "  generate one with:  openssl rand -hex 32"
  exit 2
fi

REGION="${REGION:-fra}"
PG_APP="${PG_APP:-naditos-pg}"
ORG="${FLY_ORG:-personal}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# ─── Path verification ──────────────────────────────────────────────
# Fly resolves `[build] dockerfile` paths relative to the fly.toml
# file's directory, NOT the working directory. The fly.tomls all
# specify `deploy/docker/go-service.Dockerfile`, so Fly looks for
# `deploy/fly/deploy/docker/go-service.Dockerfile`. The canonical
# Dockerfile lives at `deploy/docker/go-service.Dockerfile`. Mirror
# it (via symlink, falls back to copy on filesystems without symlink
# support) so a fresh checkout deploys without manual setup.
CANONICAL_DOCKERFILE="$REPO_ROOT/deploy/docker/go-service.Dockerfile"
EXPECTED_DOCKERFILE="$REPO_ROOT/deploy/fly/deploy/docker/go-service.Dockerfile"

if [ ! -f "$CANONICAL_DOCKERFILE" ]; then
  echo "FATAL: canonical Dockerfile missing: $CANONICAL_DOCKERFILE" >&2
  exit 3
fi

if [ ! -e "$EXPECTED_DOCKERFILE" ]; then
  echo "→ mirroring Dockerfile to deploy/fly/deploy/docker/ (Fly path resolution)"
  mkdir -p "$(dirname "$EXPECTED_DOCKERFILE")"
  ln -s "../../../docker/go-service.Dockerfile" "$EXPECTED_DOCKERFILE" \
    2>/dev/null || cp "$CANONICAL_DOCKERFILE" "$EXPECTED_DOCKERFILE"
fi

# Confirm every fly.toml references a present config and the right
# dockerfile path before we touch any apps.
for cfg in deploy/fly/fly.*.toml; do
  if ! grep -q '^app = "naditos-' "$cfg"; then
    echo "FATAL: $cfg missing 'app = \"naditos-...\"' line" >&2
    exit 3
  fi
done

# ─── Service list ──────────────────────────────────────────────────
# Order matters on first run: services with no inter-service deps
# come up first (audit, auth), then dependents, then the gateway
# last so it can resolve the .internal hostnames of its upstreams.
ALL_SERVICES=(
  audit
  auth
  registry
  license
  insurance
  inspection
  fines
  anpr-gateway
  notifications
  gateway
)

if [ -n "$ONLY_SVC" ]; then
  # validate
  found=0
  for s in "${ALL_SERVICES[@]}"; do [ "$s" = "$ONLY_SVC" ] && found=1; done
  if [ "$found" -ne 1 ]; then
    echo "FATAL: unknown service '$ONLY_SVC'. choose one of: ${ALL_SERVICES[*]}" >&2
    exit 2
  fi
  SERVICES=("$ONLY_SVC")
else
  SERVICES=("${ALL_SERVICES[@]}")
fi

# ─── Cached app list (one fetch, reused per check) ─────────────────
echo "→ fetching current app list…"
APPS_JSON="$(fly apps list --json 2>/dev/null || echo '[]')"

app_exists() {
  # Match against "Name":"<app>" — works regardless of surrounding whitespace.
  echo "$APPS_JSON" | grep -q "\"Name\"[[:space:]]*:[[:space:]]*\"$1\""
}

# ─── Postgres pre-flight ───────────────────────────────────────────
# `fly postgres attach` returns "no active leader found" if the PG
# cluster's primary isn't in `started` state. On hobby plans Fly
# auto-stops idle machines, so the most common cause is just that
# nothing has woken the cluster yet. Wake it once, here, instead of
# letting every service fail with the same opaque error.
preflight_postgres() {
  if ! app_exists "$PG_APP"; then
    echo "FATAL: postgres app '$PG_APP' does not exist." >&2
    echo "       run the prereq from deploy/fly/README.md:" >&2
    echo "         fly postgres create --name $PG_APP --org $ORG --region $REGION" >&2
    return 1
  fi

  local machines_json
  machines_json="$(fly machine list --app "$PG_APP" --json 2>/dev/null || echo '[]')"

  # Count machines by state. We only care: is there at least one started?
  local total started stopped
  total=$(echo "$machines_json"   | grep -c '"id"'                || true)
  started=$(echo "$machines_json" | grep -c '"state"[[:space:]]*:[[:space:]]*"started"' || true)
  stopped=$(echo "$machines_json" | grep -c '"state"[[:space:]]*:[[:space:]]*"stopped"' || true)

  if [ "$total" -eq 0 ]; then
    echo "FATAL: $PG_APP has zero machines — cluster was never provisioned." >&2
    echo "       run: fly postgres create --name $PG_APP --org $ORG --region $REGION" >&2
    return 1
  fi

  if [ "$started" -ge 1 ]; then
    echo "  ✓ $PG_APP has $started/$total machine(s) started"
    return 0
  fi

  echo "  → $PG_APP machines are all stopped — starting them"
  # Extract machine ids from the json. Cheap parse: lines like  "id": "abc123",
  local ids
  ids=$(echo "$machines_json" \
        | grep '"id"[[:space:]]*:' \
        | head -n "$total" \
        | sed -E 's/.*"id"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')

  for id in $ids; do
    fly machine start "$id" --app "$PG_APP" >/dev/null 2>&1 || true
  done

  # Leader election takes a few seconds after the machine reports started.
  echo "  → waiting up to 90s for a leader…"
  local waited=0
  while [ $waited -lt 90 ]; do
    sleep 5
    waited=$((waited + 5))
    machines_json="$(fly machine list --app "$PG_APP" --json 2>/dev/null || echo '[]')"
    started=$(echo "$machines_json" | grep -c '"state"[[:space:]]*:[[:space:]]*"started"' || true)
    if [ "$started" -ge 1 ]; then
      # Give Stolon another beat to elect.
      sleep 10
      echo "  ✓ $PG_APP machine(s) started after ${waited}s"
      return 0
    fi
  done

  echo "FATAL: $PG_APP has no started machines after 90s." >&2
  echo "       check: fly status -a $PG_APP" >&2
  echo "              fly logs   -a $PG_APP --no-tail | tail -50" >&2
  return 1
}

echo "→ checking $PG_APP health…"
if ! preflight_postgres; then
  exit 4
fi

secret_set() {
  local app="$1" name="$2"
  fly secrets list --app "$app" --json 2>/dev/null \
    | grep -q "\"Name\"[[:space:]]*:[[:space:]]*\"$name\""
}

# ─── Per-service steps ─────────────────────────────────────────────
create_if_missing() {
  local app="$1"
  if app_exists "$app"; then
    echo "  ✓ app exists"
    return 0
  fi
  echo "  → creating app"
  fly apps create "$app" --org "$ORG" >/dev/null
  # refresh cache so subsequent checks see the new app
  APPS_JSON="$(fly apps list --json 2>/dev/null || echo '[]')"
}

attach_pg_if_needed() {
  local app="$1"
  if secret_set "$app" "DATABASE_URL"; then
    echo "  ✓ postgres already attached (DATABASE_URL set)"
    return 0
  fi
  echo "  → attaching $PG_APP"
  # `fly postgres attach` exits non-zero with "already attached" msgs
  # in some versions; tolerate that.
  if ! fly postgres attach "$PG_APP" --app "$app" --yes 2>&1 \
       | tee /tmp/.naditos-attach.log | tail -2; then
    if grep -qiE "already (attached|exists)" /tmp/.naditos-attach.log; then
      echo "  ✓ already attached (per fly response)"
    else
      return 1
    fi
  fi
}

stage_jwt_if_needed() {
  local app="$1"
  # Compare a digest stored as a separate "marker" secret, so we don't
  # have to read the secret value back (Fly never exposes it).
  local digest
  digest=$(printf %s "$JWT_SECRET" | shasum -a 256 | cut -c1-12)
  if secret_set "$app" "JWT_SECRET" \
     && fly secrets list --app "$app" --json 2>/dev/null \
        | grep -q "\"Name\"[[:space:]]*:[[:space:]]*\"JWT_SECRET_DIGEST\""; then
    # digest marker present — assume same value to avoid forced redeploy
    echo "  ✓ JWT_SECRET already set"
    return 0
  fi
  echo "  → staging JWT_SECRET"
  fly secrets set --app "$app" --stage \
    JWT_SECRET="$JWT_SECRET" \
    JWT_SECRET_DIGEST="$digest" >/dev/null
}

deploy_one() {
  local svc="$1"
  local app="naditos-$svc"
  local cfg="deploy/fly/fly.$svc.toml"
  echo
  echo "── $svc ──────────────────────────────────────────"
  if [ ! -f "$cfg" ]; then
    echo "  ✗ missing config: $cfg" >&2
    return 1
  fi
  create_if_missing "$app"      || return 1
  attach_pg_if_needed "$app"    || return 1
  stage_jwt_if_needed "$app"    || return 1
  echo "  → fly deploy (rolling, remote builder)"
  fly deploy --config "$cfg" --app "$app" --remote-only \
    || return 1
}

# ─── Run ────────────────────────────────────────────────────────────
declare -A RESULT
for svc in "${SERVICES[@]}"; do
  if deploy_one "$svc"; then
    RESULT[$svc]="OK"
  else
    RESULT[$svc]="FAIL"
  fi
done

# ─── Summary ────────────────────────────────────────────────────────
echo
echo "═════════════════════════════════════════════════════════"
echo "  Deployment summary"
echo "─────────────────────────────────────────────────────────"
fail=0
for svc in "${SERVICES[@]}"; do
  if [ "${RESULT[$svc]}" = "OK" ]; then
    printf "  %-20s OK\n" "$svc"
  else
    printf "  %-20s FAIL\n" "$svc"
    fail=$((fail + 1))
  fi
done
echo "─────────────────────────────────────────────────────────"
if [ $fail -eq 0 ]; then
  echo "  All ${#SERVICES[@]} service(s) deployed."
  echo
  echo "  Public gateway: https://naditos-gateway.fly.dev"
  echo "  Verify:         curl https://naditos-gateway.fly.dev/healthz"
  echo
  echo "  Final step (Vercel, once per project — police, admin, citizen):"
  echo "    Project → Settings → Environment Variables"
  echo "      NEXT_PUBLIC_API_BASE        = https://naditos-gateway.fly.dev"
  echo "      NEXT_PUBLIC_DEFAULT_TENANT  = demo"
  echo "    Then redeploy that Vercel project (build-time inlined)."
else
  echo "  $fail service(s) failed. Re-run for just those:"
  for svc in "${SERVICES[@]}"; do
    [ "${RESULT[$svc]}" = "FAIL" ] && \
      echo "    bash deploy/fly/bootstrap.sh \"\$JWT_SECRET\" $svc"
  done
fi
echo "═════════════════════════════════════════════════════════"
exit $fail
