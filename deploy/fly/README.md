# NADITOS · Fly.io deployment

This folder is the blueprint for putting the Go backend on Fly.io
so the deployed Vercel apps can reach a real gateway. The frontend
fix (officer / admin / citizen) is then a one-line env-var change in
each Vercel project.

## Topology

One **public** app (the gateway) + nine **internal** apps (one per
Go service) + one Postgres cluster, all in the same Fly org. Apps
talk to each other over Fly's private 6PN network at
`<app>.internal`.

```
Vercel apps  ───────►  https://naditos-gateway.fly.dev      (public)
                          │
                          ├──► naditos-auth.internal
                          ├──► naditos-registry.internal
                          ├──► naditos-license.internal
                          ├──► naditos-insurance.internal
                          ├──► naditos-inspection.internal
                          ├──► naditos-fines.internal
                          ├──► naditos-audit.internal
                          ├──► naditos-anpr-gateway.internal
                          └──► naditos-notifications.internal
                                       │
                                       └──► naditos-pg.internal:5432
```

All ten services share **one Dockerfile** (`deploy/docker/go-service.Dockerfile`)
parameterised by `SERVICE=<name>`; one `fly.<service>.toml` per
deployment binds it to a Fly app. The shared secrets (`DATABASE_URL`,
`JWT_SECRET`) are set on every app individually because Fly secrets
are app-scoped, not org-scoped.

## One-time bootstrap

Run from the **repository root** so the Docker build context picks up
`packages/go-common` and `services/<name>` correctly.

```bash
# 1. Authenticate to your Fly org (your dashboard URL contains the
#    org slug, e.g. fly.io/dashboard/glob-cof → org "glob-cof").
fly auth login
export FLY_ORG="glob-cof"            # change to your org slug
export REGION="fra"                  # nearest Fly region

# 2. Create the Postgres cluster.
fly postgres create \
  --name naditos-pg \
  --org "$FLY_ORG" \
  --region "$REGION" \
  --initial-cluster-size 1

# 3. Apply migrations (one-off; you'll only do this on first deploy
#    or when db/migrations/ gains new files):
fly proxy 15432:5432 --app naditos-pg &
PROXY_PID=$!
sleep 2
DATABASE_URL='postgres://postgres:<password-from-step-2>@localhost:15432/naditos?sslmode=disable' \
  ./scripts/migrate.sh up
kill $PROXY_PID

# 4. Generate one shared JWT secret and deploy everything.
JWT_SECRET=$(openssl rand -hex 32)
bash deploy/fly/bootstrap.sh "$JWT_SECRET"

# 5. Point Vercel at the deployed gateway.
#    For each Vercel project (police, admin, citizen):
#      Project → Settings → Environment Variables
#      NEXT_PUBLIC_API_BASE = https://naditos-gateway.fly.dev
#      NEXT_PUBLIC_DEFAULT_TENANT = demo
#      Redeploy. (NEXT_PUBLIC_* values bake in at build time, so a
#      redeploy is required after changing them.)
```

## Deploy a single service

After bootstrap, redeploying one service is cheap:

```bash
fly deploy --config deploy/fly/fly.gateway.toml --app naditos-gateway --remote-only
```

`--remote-only` runs the build on Fly's builder, so you don't need a
local Docker daemon.

## Migrations

Migrations live in `db/migrations/` and are applied with
`scripts/migrate.sh` (forward-only psql runner). Run them from a
local shell pointed at the Fly Postgres via `flyctl proxy`:

```bash
fly proxy 15432:5432 --app naditos-pg &
DATABASE_URL='postgres://naditos:<pwd>@localhost:15432/naditos?sslmode=disable' \
  ./scripts/migrate.sh up
kill %1
```

For production-grade migration management later, switch to
`golang-migrate` or `atlas`; the file naming
(`NNNN_*.up.sql` / `.down.sql`) is already compatible.

## CORS

The gateway echoes any `Origin` header (see
`services/gateway/internal/proxy/proxy.go:57-70`) so the deployed
Vercel domains work without backend config changes. If you tighten
this later, add the Vercel deployment URLs to an allow-list there.

## Health & logs

```bash
fly status   --app naditos-gateway
fly logs     --app naditos-gateway
fly checks   --app naditos-gateway
```

Each service publishes `GET /healthz` on its `SERVICE_PORT`; the
fly.tomls wire that into the `[[services.http_checks]]` block.
