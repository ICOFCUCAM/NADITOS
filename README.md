# NADITOS — National Digital Transport Operating System

A modular, multi-tenant, multi-country government platform for vehicle
registration, driver licensing, insurance/inspection verification, police
enforcement, and fine management.

> **Status:** Phase-1 MVP foundation. This repository contains the
> production-grade skeleton (architecture, schema, services, web apps,
> deployment) — the AI-ANPR engine, payment processors, and
> court/customs integrations are wired-up stubs ready to be replaced with
> real providers per country.

## Modules (Phase 1)

| Module                | Service          | Status      |
| --------------------- | ---------------- | ----------- |
| Authentication & RBAC | `auth`           | implemented |
| Vehicle Registry      | `registry`       | implemented |
| Driver License        | `license`        | scaffold    |
| Insurance Verification| `insurance`      | scaffold    |
| Roadworthiness        | `inspection`     | scaffold    |
| Digital Fines         | `fines`          | implemented |
| ANPR Gateway          | `anpr-gateway`   | scaffold    |
| Audit Log             | `audit`          | implemented |
| Notifications         | `notifications`  | scaffold    |

## Apps

- `apps/web-admin` — Ministry / government admin dashboard (Next.js)
- `apps/police-pwa` — Officer enforcement app, plate scan + fine issuance (Next.js PWA)
- `apps/web-citizen` — Citizen portal: vehicles, fines, payments (Next.js)

## Architecture

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md),
[`docs/SECURITY.md`](docs/SECURITY.md),
[`docs/ROADMAP.md`](docs/ROADMAP.md).

```
                ┌────────────────────────────────────────────────┐
                │   Next.js apps (admin / police-PWA / citizen)  │── Vercel
                └────────────────┬───────────────────────────────┘
                                 │  HTTPS · JWT · mTLS (gateway)
                ┌────────────────┴──────────────┐
                │           API Gateway          │
                └────────────────┬──────────────┘
        ┌──────────┬─────────────┼──────────────┬──────────────┐
        ▼          ▼             ▼              ▼              ▼
     auth     registry         fines          audit       anpr-gateway
        │          │             │              │              │
        └──────────┴──────┬──────┴──────────────┴──────────────┘
                          ▼
            PostgreSQL (RLS, multi-tenant) · Redis · Object store
```

## Quick start (local dev)

```bash
make up        # postgres + redis + all services
make migrate   # run db migrations
make seed      # demo tenant + demo users + demo vehicles
make web       # run all Next.js apps
```

Then:

- Admin   → http://localhost:3000 (admin@demo / demo1234)
- Police  → http://localhost:3001 (officer@demo / demo1234)
- Citizen → http://localhost:3002 (citizen@demo / demo1234)

## Deployment

- **Next.js apps** → Vercel (`vercel.json` per app, see `docs/DEPLOY.md`)
- **Go services** → Docker images, Kubernetes manifests under `deploy/k8s/`
- **Database** → managed Postgres (Supabase / Cloud SQL / RDS / sovereign)
- **Object store** → S3-compatible for evidence (photos, signatures)

## Repository layout

```
apps/                Next.js front-ends (Vercel-deployable)
services/            Go microservices
packages/go-common/  Shared Go library (auth, db, audit, errors, logger)
db/migrations/       SQL migrations (golang-migrate compatible)
deploy/              Dockerfiles, K8s manifests, Vercel configs
docs/                Architecture, security, roadmap, deploy
```

## License

Proprietary — government deployment. Configure per-country licensing.
