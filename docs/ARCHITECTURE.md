# Architecture

## Goals

- **National scale** — millions of vehicles, millions of citizens, real-time
  police scanning, sub-second lookups.
- **Multi-tenant / multi-country** — one deployment can serve N countries
  (or N agencies in one country) with hard data isolation.
- **Anti-corruption by design** — evidence-required fines, immutable audit
  trail, duplicate-protection, anomaly detection.
- **Modular** — countries can activate only the modules they license.
- **Sovereign-deployable** — runs on EU sovereign cloud, AWS GovCloud,
  Azure Government, or on-premise.

## Service mesh

```
┌──────────────────────────── Edge ────────────────────────────┐
│ Vercel CDN + WAF (Next.js apps)                              │
│ Cloud LB + WAF (mobile / device traffic to gateway)          │
└─────────┬───────────────────────────────────────────────────┘
          │ HTTPS · JWT bearer · device-bound mTLS for officers
┌─────────▼─────────┐
│   API Gateway     │  rate-limit · auth verify · routing
└─────────┬─────────┘
   ┌──────┼──────┬───────────┬───────────┬───────────┐
   ▼      ▼      ▼           ▼           ▼           ▼
 auth  registry license  insurance  inspection    fines
                                                    │
                                                    ▼
                                          ┌──────────────────┐
                                          │  audit (append)  │
                                          └──────────────────┘
   ┌────────────┬─────────────┐
   ▼            ▼             ▼
 anpr-       notifications  analytics
 gateway                    (read-only views)
```

Every service is a self-contained Go binary, owns its DB schema (separate
schemas in one Postgres cluster for Phase 1; can be split per service when
scale requires it), exposes HTTP+gRPC, and emits structured JSON logs +
OpenTelemetry traces.

## Data model — multi-tenant via Postgres RLS

Every domain row carries a `tenant_id` (typically a country/agency code).
Every authenticated request sets:

```sql
SET LOCAL app.tenant_id = '<tenant>';
SET LOCAL app.user_id   = '<user>';
SET LOCAL app.role      = '<role>';
```

Row-Level-Security policies on every table enforce isolation at the
database, not in application code. A service cannot accidentally leak
tenant data even if a query forgets the WHERE clause.

## Vehicle status engine

Each vehicle has a derived status `green | yellow | red | black` computed
from:

- insurance.expires_at vs now (red if expired)
- inspection.expires_at vs now (red if expired)
- registration.expires_at vs now
- tax.paid_through vs now
- police_alerts (stolen / wanted → black)
- pending_fines past escalation threshold

The status is computed in the registry service via a SQL view
`v_vehicle_status` so it's always live and never stale.

## Fine issuance — anti-corruption gates

`POST /fines` requires:

1. `vehicle_id` (looked up via plate or ANPR)
2. `offence_code` (enum from regulation engine — officer cannot type one)
3. `evidence` — at least one S3 object key (photo) with EXIF/GPS
4. `geo` — `{ lat, lng, accuracy_m }`
5. `officer_id` from JWT
6. `device_id` from JWT (device-bound)
7. **Server computes the fine amount** from the regulation engine.
   The officer cannot set or override the amount.

Server-side rejections:

- duplicate within `duplicate_window_min` for same `vehicle_id × offence_code`
- evidence missing or unverifiable
- officer outside assigned jurisdiction (when configured)
- vehicle status mismatch (e.g. expired-insurance offence on green vehicle)

## Audit log — immutable + hash-chained

`audit_events` is append-only. Each row stores:

- `actor_*` (user, role, device, IP, tenant)
- `action`, `resource_type`, `resource_id`
- `before` / `after` JSONB
- `prev_hash`, `hash` — hash chain over the canonical row encoding

Tamper detection: any row mutation breaks the chain and is detected by a
periodic verifier job. Inserts go through the `audit` service which is
the only role allowed to write the table.

## Regulation engine

Each country plugs in:

- `offences[]` — code, name, description, base_amount, currency,
  expr (rule, e.g. `vehicle.insurance.expired`), localized_name
- `escalation_stages[]` — durations + multipliers
- `vehicle_categories[]`, `license_classes[]`
- `inspection_intervals` per category

Stored in `regulation_*` tables, hot-reloaded by services.

## Internationalization

- All user-facing strings live in `apps/*/locales/{en,fr,de,es,no,ar}.json`
- Right-to-left support for Arabic
- Currency, date, plate-format are per-tenant, not per-user

## Offline mode (police PWA)

Police PWA caches:

- Last 14 days of plate-status (encrypted IndexedDB, AES-GCM derived from
  device key)
- Pending fine drafts queued and replayed on reconnect

Server reconciles drafts: re-validates evidence, re-applies regulation
engine, rejects duplicates, returns canonical fine IDs.

## Deployability

- Every service ships a `Dockerfile` and a `deploy/k8s/<service>.yaml`
  (deployment + service + HPA + network policy)
- Frontends ship `vercel.json` and standard Next.js build
- See [`DEPLOY.md`](DEPLOY.md)
