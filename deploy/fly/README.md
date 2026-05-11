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

If `bootstrap.sh` failed partway through, **re-run it as-is**: it
detects existing apps, attached Postgres, and staged secrets, and
only does the work that's still needed. To target a single failed
service, pass it as the second argument:

```bash
bash deploy/fly/bootstrap.sh "$JWT_SECRET" registry
```

## Dockerfile path quirk

Fly resolves the `[build] dockerfile` path in `fly.toml` **relative
to the fly.toml file's directory**, not your working directory.
Every `fly.<service>.toml` here references
`deploy/docker/go-service.Dockerfile`, which Fly therefore looks for
at `deploy/fly/deploy/docker/go-service.Dockerfile`. The canonical
copy lives at `deploy/docker/go-service.Dockerfile` (the same one
`docker-compose.yml` uses).

`bootstrap.sh` mirrors the canonical Dockerfile to the
Fly-expected path on first run (symlink preferred, copy fallback)
so you don't have to think about it. The mirrored file is gitignored
to avoid drift.

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

## "Error: no active leader found"

Every service fails on the `attaching naditos-pg` step with
`Error: no active leader found`. Cause: the Postgres cluster has no
machine in `started` state, so there's no Stolon leader for
`fly postgres attach` to talk to. On hobby plans this is almost
always **idle auto-stop** — Fly suspends the PG VMs after inactivity.

`bootstrap.sh` now does a Postgres pre-flight on every run: it
auto-starts any stopped machines and waits up to 90s for a leader
before touching the services. If that pre-flight fails, investigate
directly:

```bash
fly status       -a naditos-pg
fly machine list -a naditos-pg
fly logs         -a naditos-pg --no-tail | tail -50
```

Common outcomes:

* **Zero machines listed** → the `fly postgres create` prereq never
  completed. Re-run it.
* **Machines `stopped`** → `fly machine start <id> -a naditos-pg`,
  wait ~30s, re-run bootstrap. (The pre-flight should handle this
  automatically; do it by hand only if it didn't.)
* **Machines `started` but logs show Postgres crashing** → likely a
  volume / WAL issue; check `fly logs` for the specific error.

## Health & logs

```bash
fly status   --app naditos-gateway
fly logs     --app naditos-gateway
fly checks   --app naditos-gateway
```

Each service publishes `GET /healthz` on its `SERVICE_PORT`; the
fly.tomls wire that into the `[[services.http_checks]]` block.

## Shared database, JWT, and admin bootstrap

The `fly postgres attach` shortcut creates **one database per service**
(`naditos_auth`, `naditos_registry`, …). That breaks the schema's
shared-tenant design — `users` and `vehicles` end up in different
databases, so RLS policies and foreign keys can't span them. Use one
**shared `naditos` database** with one **`naditos_admin`** role
instead. The `bootstrap.sh` Postgres step writes a single
`DATABASE_URL` secret to every service:

```
postgres://naditos_admin:<pw>@naditos-pg.flycast:5432/naditos?sslmode=disable
```

The `naditos_admin` role is created by migration `0001_init.up.sql`
with `BYPASSRLS` (it's the row-security escape hatch the auth service
uses at login time). Set its password and grants once, after migrations:

```sql
ALTER ROLE naditos_admin LOGIN PASSWORD '<value>';
ALTER ROLE naditos_admin BYPASSRLS;
GRANT ALL ON ALL TABLES    IN SCHEMA public TO naditos_admin;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO naditos_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES    TO naditos_admin;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO naditos_admin;
```

### `ADMIN_BOOTSTRAP_KEY`

`POST /v1/admin/users` is the only way to create the very first admin
user (no admin JWT exists yet to authorise the call). Both the auth
handler and the gateway accept the request only if the request carries
a header `X-Admin-Bootstrap-Key: <secret>` whose value matches the
`ADMIN_BOOTSTRAP_KEY` env var on **both** apps. Once you've created at
least one admin user you can rotate or unset the key; subsequent
admin-management calls authenticate with that admin's JWT. Generate
and set on every service:

```bash
ADMIN_BOOTSTRAP_KEY=$(openssl rand -hex 32)
for app in naditos-auth naditos-registry naditos-license naditos-insurance \
           naditos-inspection naditos-fines naditos-audit \
           naditos-anpr-gateway naditos-notifications naditos-gateway; do
  fly secrets set -a "$app" ADMIN_BOOTSTRAP_KEY="$ADMIN_BOOTSTRAP_KEY"
done

ADMIN_BOOTSTRAP_KEY="$ADMIN_BOOTSTRAP_KEY" \
API=https://naditos-gateway.fly.dev \
TENANT=demo \
bash scripts/seed.sh
```

`scripts/seed.sh` refuses to run without the env var so it can't
silently miss the new gate.

## Things that bite on first deploy

### "Allocate dedicated IPv4 and IPv6?" prompt

`fly deploy` on a service that's never been deployed prompts for
public IPs. **Only the gateway needs them — answer `n` for everything
else**. Internal services are reachable on `<app>.internal` over Fly's
6PN private network. If you accidentally allocate public IPs to an
internal service, they cost $2/mo each and serve no traffic; release
them:

```bash
fly ips list -a naditos-<service>
fly ips release <ip> -a naditos-<service>
```

### `panic: missing env var: DATABASE_URL` on the gateway

The gateway's `cmd/server/main.go` calls `config.MustLoad` (no DB
required), but earlier revisions used the strict `MustLoadWithDB`. If
you see this panic on the gateway specifically, the deployed binary is
the older code — `fly deploy --no-cache` to rebuild fresh.

### Postgres "no active leader found"

Hobby PG auto-stops when idle. Both `fly postgres attach` and the
internal services need a leader. If `fly status -a naditos-pg` shows
a `stopped` machine, start it manually:

```bash
fly machine start <pg-machine-id> -a naditos-pg
sleep 30
fly status -a naditos-pg     # want STATE=started, ROLE=primary
```

## Verifying a deploy end-to-end

```bash
API=https://naditos-gateway.fly.dev
TENANT=demo

# Health
curl -fsS "$API/healthz"       # → {"ok":true}

# Login
TOK=$(curl -sS -X POST "$API/v1/auth/login" \
  -H 'Content-Type: application/json' -H "X-Tenant-Id: $TENANT" \
  -d '{"email":"officer@demo","password":"demo1234"}' | jq -r .access_token)
echo "token len: ${#TOK}"      # should be 200+

# Vehicle lookup (the police PWA's primary path)
curl -sS -H "Authorization: Bearer $TOK" -H "X-Tenant-Id: $TENANT" \
     "$API/v1/vehicles/by-plate/STOLEN-1" | jq .
# Expect: JSON with "plate":"STOLEN-1","is_stolen":true,"status":"black"
```

## Confirming a deploy is actually running the latest code

`server.Run` (called by every backend service on boot) emits a structured
log line with the git SHA the binary was built from:

```json
{"level":"INFO","msg":"boot","service":"auth",
 "git_sha":"ee829f8a","git_sha_full":"ee829f8a9a5a8a...","git_modified":false,...}
```

To confirm an `fly deploy` actually pushed the new image:

```bash
EXPECTED=$(git rev-parse --short=8 HEAD)
ACTUAL=$(fly logs -a naditos-auth --no-tail | grep '"msg":"boot"' \
          | tail -1 | jq -r .git_sha)
echo "expected $EXPECTED   actual $ACTUAL"
test "$EXPECTED" = "$ACTUAL" && echo OK || echo "STALE — re-deploy with --no-cache"
```

If `git_modified: true` shows up in production, someone built and shipped
from a dirty checkout. The SHA in `git_sha` does not fully describe what's
running. Treat that as an incident: rebuild from a clean checkout.

## Secret rotation runbooks

The platform uses three shared secrets across the backend services. Each
needs a different rotation procedure.

### `JWT_SECRET` rotation

The signing key for every access and refresh token. **Every backend
service must agree on the value** or tokens issued by `auth` won't
verify at the gateway, registry, etc.

**When**: on suspected compromise, when an engineer who had access leaves,
and as scheduled hygiene every 90 days. **Cost**: every active session
becomes invalid; all users must re-login.

```bash
# 1. Generate a fresh secret (32 random bytes, hex-encoded)
NEW_JWT=$(openssl rand -hex 32)

# 2. Stage on every backend service simultaneously. --stage means the
#    secret is queued but no machine restarts yet, so the rollout can
#    happen as a single atomic-ish flip across services.
for app in naditos-auth naditos-registry naditos-license naditos-insurance \
           naditos-inspection naditos-fines naditos-audit \
           naditos-anpr-gateway naditos-notifications naditos-gateway; do
  fly secrets set --stage -a "$app" JWT_SECRET="$NEW_JWT"
done

# 3. Deploy the staged secrets in a tight loop. Order: backend first,
#    gateway last — so a transient mismatch hits internal calls (which
#    can retry) rather than client-facing requests.
for app in naditos-auth naditos-registry naditos-license naditos-insurance \
           naditos-inspection naditos-fines naditos-audit \
           naditos-anpr-gateway naditos-notifications; do
  fly secrets deploy -a "$app"
done
fly secrets deploy -a naditos-gateway

# 4. Verify with a fresh login
curl -sS -X POST https://naditos-gateway.fly.dev/v1/auth/login \
  -H 'Content-Type: application/json' -H 'X-Tenant-Id: demo' \
  -d '{"email":"officer@demo","password":"demo1234"}' | jq -r '.access_token | length'
# Expect a non-zero number (the new token length).

# 5. Communicate the rotation. All in-flight sessions are invalid; the
#    next request from any logged-in user will get a 401 with
#    code=unauthorized and they'll be redirected to login.
```

In a sovereign deployment, the staged → deploy step happens via the secret
manager's audit-logged path, not Fly secrets. Fly is the transitional shape.

### `ADMIN_BOOTSTRAP_KEY` lifecycle

The shared secret that the gateway and the auth service both check for
`POST /v1/admin/users` when no admin JWT is available yet. Used **once**,
on first deploy, to bootstrap the first admin. After that, all
admin-creation goes through admin JWTs and the bootstrap key should be
rotated to empty.

| When | What |
|---|---|
| First deploy | Generate; set on auth + gateway via `fly secrets set` |
| `scripts/seed.sh` runs once successfully | First admin exists; you can rotate now |
| **Rotate to empty** as soon as practical | `fly secrets unset -a naditos-auth ADMIN_BOOTSTRAP_KEY && fly secrets unset -a naditos-gateway ADMIN_BOOTSTRAP_KEY` |

The auth handler closes the bootstrap path entirely if the env var is
empty (`services/auth/internal/api/api.go:adminAuthorized`). After that,
admin user creation requires an admin JWT — which means somebody who
already has admin role makes the request from the admin UI.

If you need to add a new admin from scratch later (e.g. all admins
locked out): generate a new `ADMIN_BOOTSTRAP_KEY`, repeat the seeding
flow, then rotate it back to empty.

### `DATABASE_URL` rotation

The Postgres connection string includes the password. Rotate by:

1. `fly postgres detach naditos-pg --app <service>` (drops the per-app
   role + secret).
2. `fly postgres attach naditos-pg --app <service> --database-user
   <new-name>` (creates a fresh role with a new password and writes the
   new secret).
3. Re-run for every service that needs DB access (everyone except gateway).

**Caveat**: the consolidation work earlier in this project moved every
service to the shared `naditos` database under `naditos_admin`. If you
rotate by re-attaching, you'll get one fresh per-service role per
service — undoing the consolidation. Until we automate the shared-DB
rotation, the right approach is:

```sql
-- in psql as postgres superuser
ALTER ROLE naditos_admin PASSWORD 'new-strong-password';
```

Then update `DATABASE_URL` on every service:

```bash
NEW_URL='postgres://naditos_admin:new-strong-password@naditos-pg.flycast:5432/naditos?sslmode=disable'
for app in naditos-auth naditos-registry naditos-license naditos-insurance \
           naditos-inspection naditos-fines naditos-audit \
           naditos-anpr-gateway naditos-notifications; do
  fly secrets set -a "$app" DATABASE_URL="$NEW_URL"
done
```

Each `fly secrets set` triggers a rolling restart on the target service.
A full rotation across 9 services takes ~2 minutes wall clock.
