#!/usr/bin/env bash
# NADITOS · Fly.io bootstrap.
#
# Usage:
#   bash deploy/fly/bootstrap.sh "$JWT_SECRET"
#
# Pre-requisites you must do by hand first:
#   1. fly auth login
#   2. fly postgres create --name naditos-pg --region fra (or your region)
#   3. Run db migrations once: see deploy/fly/README.md "Migrations" section.
#
# Idempotent: re-running skips apps that already exist and just runs
# fly deploy on each one again.
set -euo pipefail

JWT_SECRET="${1:-}"
if [ -z "$JWT_SECRET" ]; then
  echo "usage: $0 <jwt-secret>"
  echo "  generate one with:  openssl rand -hex 32"
  exit 2
fi

REGION="${REGION:-fra}"
PG_APP="${PG_APP:-naditos-pg}"
ORG="${FLY_ORG:-personal}"

cd "$(git rev-parse --show-toplevel)"

# Order matters: services audit/auth come up first because they have
# no inter-service deps, then the rest, then the gateway last.
SERVICES=(
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

create_if_missing() {
  local app="$1"
  if fly apps list --json | grep -q "\"Name\":\"$app\""; then
    echo "✓ app $app already exists"
  else
    echo "→ creating app $app"
    fly apps create "$app" --org "$ORG" >/dev/null
  fi
}

attach_pg() {
  local app="$1"
  # `fly postgres attach` is idempotent — it sets DATABASE_URL secret.
  echo "→ attaching $PG_APP to $app"
  fly postgres attach "$PG_APP" --app "$app" --yes >/dev/null 2>&1 || true
}

set_secrets() {
  local app="$1"
  echo "→ setting JWT secret on $app"
  fly secrets set --app "$app" JWT_SECRET="$JWT_SECRET" --stage >/dev/null
}

deploy() {
  local svc="$1"
  local app="naditos-$svc"
  local cfg="deploy/fly/fly.$svc.toml"
  echo "── $svc ──────────────────────────────────────────"
  create_if_missing "$app"
  attach_pg "$app"
  set_secrets "$app"
  echo "→ deploying $app"
  fly deploy --config "$cfg" --app "$app" --remote-only --strategy immediate
  echo
}

for svc in "${SERVICES[@]}"; do
  deploy "$svc"
done

echo
echo "═════════════════════════════════════════════════════════"
echo "  All services deployed."
echo
echo "  Public gateway: https://naditos-gateway.fly.dev"
echo
echo "  Next steps:"
echo "    1. Verify:  curl https://naditos-gateway.fly.dev/healthz"
echo "    2. In each Vercel project (police, admin, citizen),"
echo "       set NEXT_PUBLIC_API_BASE=https://naditos-gateway.fly.dev"
echo "       and redeploy."
echo "═════════════════════════════════════════════════════════"
