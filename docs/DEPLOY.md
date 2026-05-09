# Deployment

## Topology

| Component             | Where it runs                  |
| --------------------- | ------------------------------ |
| Next.js apps (3)      | **Vercel** (one project per app) |
| Go microservices (9)  | Kubernetes (any) / Cloud Run / Fly.io |
| PostgreSQL            | Managed (Cloud SQL / RDS / Supabase / sovereign) |
| Redis                 | Managed (ElastiCache / Upstash) |
| Object storage        | S3-compatible (S3 / GCS / MinIO / sovereign) |

> Vercel does **not** run Go microservices. The frontends point at the
> services via `NEXT_PUBLIC_API_BASE`, which should resolve to the API
> gateway in front of the Go services.

## Vercel — frontends

Each app under `apps/<app>` ships a `vercel.json`. The repo is a pnpm
workspace, so each Vercel project must be configured with a **Root
Directory** of `apps/<app>` — without it, Vercel runs the install/build
from the repo root and the per-app `vercel.json` never executes.

### One-time setup per project (admin / police / citizen)

Vercel dashboard → **Add New Project** → import the GitHub repo, then:

1. **Root Directory**: `apps/web-admin` (or `apps/police-pwa` / `apps/web-citizen`)
2. **Framework Preset**: leave on Next.js (auto-detected from `vercel.json`)
3. **Environment Variables** (Production scope):
    - `NEXT_PUBLIC_API_BASE = https://naditos-gateway.fly.dev`
    - `NEXT_PUBLIC_DEFAULT_TENANT = demo`
4. Deploy.

### CLI alternative

From each app directory:

```bash
cd apps/<app>
vercel link
vercel env add NEXT_PUBLIC_API_BASE production
vercel env add NEXT_PUBLIC_DEFAULT_TENANT production
vercel deploy --prod
```

### Changing env vars

`NEXT_PUBLIC_*` values are inlined at build time. After changing an env
var, redeploy with **"Use existing Build Cache"** UNCHECKED — otherwise
the old value stays in the bundle.

Environment variables required per app:

| Var                          | All apps |
| ---------------------------- | -------- |
| `NEXT_PUBLIC_API_BASE`       | yes      |
| `NEXT_PUBLIC_DEFAULT_TENANT` | yes      |

## Kubernetes — services

Manifests under `deploy/k8s/`. Apply in order:

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/10-config.yaml      # ConfigMap + Secret refs
kubectl apply -f deploy/k8s/20-postgres.yaml    # for dev clusters only
kubectl apply -f deploy/k8s/30-services.yaml    # all 9 services
kubectl apply -f deploy/k8s/40-gateway.yaml     # nginx/ingress
```

Each service Deployment includes:

- `readinessProbe` `GET /healthz`
- `livenessProbe` `GET /livez`
- HPA on CPU 70% (3–20 replicas)
- NetworkPolicy allowing only the gateway + audit + Postgres
- ServiceAccount with no cluster-wide perms

## Database migrations

```bash
DATABASE_URL=... ./scripts/migrate.sh up
```

Migrations are golang-migrate compatible (`<n>_<name>.up.sql` /
`.down.sql`).

## Secrets

Production secrets must come from a secret manager, never the env file.
The Go `config` package supports `secret://aws/<name>`,
`secret://gcp/<project>/<name>`, `secret://vault/<path>` URIs.

## Domain / TLS

- `admin.<country>.naditos.gov` → Vercel
- `police.<country>.naditos.gov` → Vercel (with PWA installable)
- `my.<country>.naditos.gov` → Vercel
- `api.<country>.naditos.gov` → Ingress / API gateway
