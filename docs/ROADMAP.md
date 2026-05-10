# NADITOS — Strategic Architecture Roadmap

> **Status**: Living document. Updated PR-by-PR.
> **Audience**: Engineering, ministry operations, eventual sovereign-deployment partners.
> **Not**: a product brochure, an AI sales pitch, or a guarantee of delivery dates.

---

## 0. Executive summary

NADITOS is a multi-tenant transport-governance platform. The backend is ten Go
microservices behind a single API gateway, talking to a multi-tenant Postgres
with row-level security. Six Next.js apps sit on top, each scoped to a
different operator persona. The platform is currently usable end-to-end for
the *police officer* and *ministry admin* paths against a demo tenant, with a
Norwegian-format tenant seeded as a multi-jurisdiction example.

**The hard architectural work is done.** What remains is:

1. **Reliability** — eliminate the operational footguns (PG auto-stop, missing
   observability, manual deploy steps).
2. **Government workflow depth** — the apps are shells; ministry users need
   real screens for the work they actually do.
3. **Multi-jurisdiction maturity** — onboard the next country in 30 minutes,
   not a 5-hour SQL session.
4. **Auditability and anti-corruption** — the audit ledger exists; the
   review surface and detection rules don't.

This document is the anchor against which every subsequent PR is justified.
A change that doesn't move us along one of the six phases below should
have a paragraph in its PR description explaining why it's worth doing
anyway.

---

## 1. Current reality (honest assessment)

### 1.1 What works today (verified end-to-end this session)

| Layer | Status |
|---|---|
| Fly.io 10-service backend (gateway + 9 internal) | Healthy |
| Shared `naditos` Postgres with all 12 migrations applied | Healthy |
| JWT issuance + verification + role-based gateway routing | Working |
| `/v1/admin/users` with `ADMIN_BOOTSTRAP_KEY` seeding flow | Working |
| Per-tenant `plate_regex` enforcement on `POST /v1/vehicles` | Working |
| Tenant config (`plate_regex`, currency, country) on session | Working |
| Multi-tenant seed: `demo` (XX/EUR) + `no` (NO/NOK) | Working |
| Police PWA on Vercel | Live |
| Police PWA login → plate lookup → flagged-vehicle render | Verified |
| Append-only audit ledger with hash-chain `verify` endpoint | Live |

### 1.2 Phase 0 — what's already shipped

The first build of the platform delivered the data model, the service
decomposition, and the initial frontends. Inventory below; this is the
foundation Phase 1 builds *on*, not *toward*.

**Backend, data, and platform**

- [x] Multi-tenant Postgres schema with row-level security
- [x] Auth + JWT + RBAC (constant-time admin bootstrap key, admin-JWT bypass)
- [x] Vehicle Registry with status engine
- [x] Fines engine with evidence-required + duplicate-protection
- [x] Append-only hash-chained audit log + `verify` endpoint
- [x] Driver license module — full lifecycle, demerit engine, QR/NFC verify
- [x] Insurance verification module — provider router + retry queue + health monitor + worker persists policy
- [x] Roadworthiness (inspection) module — same connector framework + worker persists record
- [x] ANPR gateway — async pipeline with outbox-routed event emission
- [x] Country regulation packs — versioned manifest + hot-reloading loader
- [x] Provider connector framework — retry queue, health monitor, country router
- [x] Domain event bus — in-process + outbox + relay + cross-process consumers
- [x] OpenAPI 3.1 spec for the gateway
- [x] Observability primitives — request id, structured JSON logs, `/metrics`
- [x] Notifications — consumer drains `event_outbox`; 7 renderers; citizen inbox
- [x] Vehicle ownership transfer (citizen → citizen) with code + 7-day expiry
- [x] Audit anomaly detection — z-score + cancel-rate detectors → `audit_alerts`
- [x] ANPR alerts → `audit_alerts` (stolen / seized / wanted vehicle scans)
- [x] Evidence retention reaper — `sealed_at` + storage delete + audit custody
- [x] Payment webhook with signature verification + idempotent paid transition
- [x] Race detector + govulncheck in CI; full-package test coverage in `packages/go-common`
- [x] Per-tenant `plate_regex` enforcement on vehicle create + client-side hint in admin app
- [x] Norway demo tenant proving multi-jurisdiction works without service redeploy

**Frontends**

- [x] `apps/police-pwa` — login + plate scan + lookup + fine-issuance flow (live on Vercel)
- [x] `apps/web-admin` — dashboard + vehicles list + create-vehicle form
- [x] `apps/web-citizen` — vehicles + fines + pay-stub
- [x] `apps/web-inspection` — home dashboard wired to live registry data
- [x] `apps/web-insurance` — home dashboard wired to per-tenant provider health
- [x] `apps/web-compliance` — home dashboard wired to audit events + alerts + verify

**Deployment**

- [x] `docker-compose.yml` for local dev
- [x] Fly.io fly.toml per service + `bootstrap.sh` driver
- [x] K8s skeleton manifests (parity with Fly is Phase 6)
- [x] Vercel pnpm-workspace setup with per-app `vercel.json`

### 1.3 What's scaffolded but inert

| App | Scaffold | Real screens |
|---|---|---|
| `apps/web-admin` (Ministry Command) | ✓ | Vehicles list + create only; everything else is the navigation stub |
| `apps/web-citizen` | ✓ | Sidebar nav only |
| `apps/web-inspection` | ✓ | Home dashboard wired to live data; subpages 404 |
| `apps/web-insurance` | ✓ | Home dashboard wired to live data; subpages 404 |
| `apps/web-compliance` | ✓ | Home dashboard wired to live data; subpages 404 |

### 1.4 What does not exist yet

- A dedicated `naditos-regulations` service (regulations live in tables read
  by `fines` and `registry`).
- Centralised observability (Loki / Prometheus / Grafana / OTel collector).
- **Required** CI gating on `main`. The workflow at
  `.github/workflows/ci.yml` already runs `go build + vet`, integration
  tests against a real Postgres, an end-to-end smoke, govulncheck, and
  per-app `pnpm build` on every PR — but **branch protection isn't
  enabled**, so a green CI isn't required for merge. Recent merges
  shipped without waiting for the workflow at all. See
  `docs/BRANCH-PROTECTION.md` for the one-time UI setup.
- Branch protection on `main`.
- Real role taxonomy beyond `admin` / `officer` / `citizen` / `court` —
  no `inspector` / `auditor` / `insurance_partner` roles, so the consoles
  built for those personas are open to any authenticated user.
- AI / ML pipelines of any kind (intentionally — Phase 5).
- Multi-region failover / sovereign-deployment manifests (intentionally — Phase 6).
- Vercel projects for `web-admin`, `web-citizen`, `web-inspection`,
  `web-insurance`, `web-compliance` — only `police-pwa` is on Vercel.
- Email/SMS deliverability — `notifications` service exists but isn't wired
  to a real provider (Sender contract ready).
- A staging environment — there is only prod.

### 1.5 Known technical debt

1. **Postgres on Fly's hobby tier auto-stops on idle.** First request after
   long idle takes 20-40s and intermittently fails dependent services'
   health checks. Single biggest source of "broken-looking" behaviour.
   → Phase 1 fix.
2. **Per-service DBs vs shared DB**: we ran a manual migration to
   consolidate to a single `naditos` database when `fly postgres attach`
   created per-service ones. The bootstrap script and docs have been
   updated, but the recovery flow if anyone runs the original `bootstrap.sh`
   on a fresh org is undocumented.
3. **`scripts/smoke.sh` only works locally**, not against the live gateway.
4. **`flagged=1` filter on `/v1/vehicles`** matches `is_stolen|is_seized|
   is_wanted` only, not the broader `status='red'`. Frontend was patched;
   the API itself is unchanged.
5. **Mux pattern alignment** — the `"GET  /v1/..."` double-space bug across
   five services (`933b29a`) was caught by accident. There's no linter
   forbidding it.
6. **`config.MustLoadWithDB` panics at startup** if `DATABASE_URL` is empty.
   Correct for DB-bound services but turned a misconfig into a crash-loop
   earlier. There's no health-degraded-but-running mode.

### 1.6 Known operational risks (top 5)

| Risk | Impact | Likelihood | Mitigation in this doc |
|---|---|---|---|
| PG cold-start locks the entire stack | High | Always (today) | §3 — move PG off hobby |
| Single shared Postgres = single blast radius | High | Possible | §10 — per-region clusters |
| `JWT_SECRET` rotation procedure undocumented | High | Possible | §8 — secret management |
| `ADMIN_BOOTSTRAP_KEY` left enabled forever | Medium | Possible | §8 — rotation runbook |
| CI runs but isn't required for merge ⇒ regressions can ship | Medium | Probable | §9 + docs/BRANCH-PROTECTION.md |

---

## 2. Phase 1 — Stabilization (now → 4 weeks)

**Goal**: a stable government-operations platform on top of which ministries
can be confident running enforcement workflows. Reliability first; features
second.

### Acceptance criteria

A successful Phase 1 means:

- Login returns 200 within 1s warm, 3s cold; cold-start does not need human
  intervention.
- Every backend service emits structured access logs with request-id,
  trace-id, tenant-id, status, duration on every request.
- Every backend service has passing readiness AND liveness probes.
- Every PR to `main` runs `go test ./...` + `pnpm build` and blocks on red.
- Branch protection on `main` is enabled.
- A documented incident-response runbook for the top-5 operational risks.
- `JWT_SECRET` rotation procedure documented + tested in staging.

### Tasks, ordered

1. **Move PG off hobby auto-stop.** Either Fly MPG (managed) or
   `min_machines_running ≥ 1` on the existing `naditos-pg` machine.
   Estimated: 30 min (MPG) or 2 hours (self-managed). **Highest leverage.**
2. **Branch protection on `main` + required CI**. GitHub Actions:
   `go test ./...` per service, `pnpm build` per app, `golangci-lint`.
   Estimated: 2-3 hours.
3. **Single startup manifest per service**. Each `cmd/server/main.go` should
   log the same structured fields on boot (build sha, port, default tenant,
   JWT-secret length, DB host). Already done for auth; mirror to the others.
4. **Panic recovery in every service's mux**. Already in
   `packages/go-common/server/Mount`; verify all services use `Mount`
   (not raw `ListenAndServe`).
5. **Document the `JWT_SECRET` rotation procedure** in `deploy/fly/README.md`.
6. **Document `ADMIN_BOOTSTRAP_KEY` lifecycle** — generated on first deploy,
   used by `seed.sh`, **rotated to empty** once the first admin exists.
7. **Stale-binary guard**: include the git sha in the boot log. If the boot
   log doesn't show the latest sha, operators know the deploy didn't take.
8. **A `make smoke` against the live gateway** with curl-driven tests for
   login, plate lookup, audit verify, producing a one-line PASS/FAIL.

### Tasks deferred to later phases

- Anything visual (frontend depth) → Phase 3.
- Real Prometheus / Loki / Grafana → Phase 2 proper.
- AI hooks → Phase 5.

---

## 3. Phase 2 — Observability & operations (4 → 10 weeks)

**Goal**: a ministry operator can answer "is the system working right now,
and if not, why?" in under 60 seconds without engineering involvement.

### 3.1 Stack

| Layer | Tool | Why this and not the alternative |
|---|---|---|
| Logs | **Loki** | Cheap to operate, queryable from Grafana, structured-log-friendly, self-hosts in sovereign deploys. |
| Metrics | **Prometheus** | Industry default. The in-process counter we already publish from `observability.MetricsHandler` is Prom-shaped. |
| Tracing | **OpenTelemetry collector → Tempo** | OTel is the only protocol that survives a backend swap. Every service already injects request-id; promote that to a real W3C `traceparent`. |
| Dashboards | **Grafana** | Single pane for logs + metrics + traces. |
| Alerting | **Grafana Alerting → notifications service → email/SMS** | Reuse the notifications service we already deployed. |

### 3.2 What every service must expose

- `GET /healthz` — 200 if listening (already present).
- `GET /livez` — 200 if can begin a no-op DB transaction (verify all 9 services).
- `GET /metrics` — Prometheus exposition (already present per `MetricsHandler`).
- Structured JSON logs to stdout (already present per `logger.New`).
- Trace-context propagation (`Traceparent` in / out — partial today).

### 3.3 SLOs to publish

- **Auth login**: p99 < 500ms warm, < 5s cold; error rate < 0.1%.
- **Vehicle by-plate**: p99 < 200ms warm; error rate < 0.5%.
- **Audit `/verify`**: p99 < 2s; error rate < 1%.
- **Gateway availability**: 99.5% (sovereign tier 99.9%).

### 3.4 Architecture sketch

```
[ all 9 services ] ──stdout──> [ Loki ]
        │                          │
        ├──/metrics──> [ Prometheus ]
        │                          │
        └──OTLP/grpc──> [ otel-collector ] ──> [ Tempo ]
                                                   │
[ Grafana ] <──────────────── all three ───────────┘
    │
    └─alerts──> [ naditos-notifications ] → email/SMS → on-call
```

Otel-collector and Loki/Prom/Grafana/Tempo run as four extra Fly apps in
Phase 2. In Phase 6 they migrate to whatever sovereign stack the ministry
has, **without changing service code** — that's the OTLP point.

### 3.5 Environment management

- Shared `.env.example` under `deploy/`.
- Staging vs production split via separate Fly orgs (or namespaces
  inside one).
- Per-tenant config still lives in the country pack manifest, not env vars.

---

## 4. Phase 3 — Frontend ecosystem completion (parallel to Phase 2)

The shells are scaffolded; depth is what's missing. Implementation **order**:

### 4.1 Priority 1 — Ministry Command (`web-admin`)

The "brain" of the ecosystem. Must be the depth-leader.

**Minimum viable** (4-6 weeks):

- National stats page (vehicles total, fines outstanding, alerts open) — done
- Vehicles search + flagged filter + create — done
- Officer activity (per-officer fine count, cancellation rate)
- Disputes queue (read/decision)
- Provider health board (per-tenant insurance/inspection health)
- Country-pack viewer (read-only, structured render of the manifest)

**Full scope**:

- Live ANPR feed (websocket from `anpr-gateway`)
- Roadworthiness analytics
- Border-checkpoint monitoring
- Anomaly drilldown linked to compliance alerts
- Country-pack editor (with regex live-preview)

### 4.2 Priority 2 — Citizen Portal (`web-citizen`)

Public-facing, anti-corruption keystone. Must be ruthlessly simple.

**Minimum viable** (3-4 weeks):

- View my vehicles + ownership transfer flow
- Pay a fine (Stripe sandbox first; per-country provider plug-in shape)
- Dispute a fine (file form → admin queue)
- View my driver license + endorsements

**Full scope**:

- Renew license (with document upload)
- Track inspection appointment
- Verify officer identity by badge number
- Report corruption (anonymous channel, hash-chained submission)
- Push notifications via the notifications service

### 4.3 Priority 3 — National Audit & Compliance (`web-compliance`)

Sovereign accountability layer. Read-only first; investigation tools later.

**Minimum viable** (2-3 weeks):

- Audit ledger live tail (already wired in home dashboard)
- Per-officer activity drill-down
- Hash-chain verification with visible last-checked timestamp
- Open-alerts queue (already wired)

**Full scope**:

- Investigation case management (link audit events → case → close-out report)
- Officer misconduct detection rules (configurable from admin)
- Pattern-match for "officer cancels fines for the same plate repeatedly"
- Court-order redaction workflow

### 4.4 Priority 4 — Inspection Authority (`web-inspection`)

**Minimum viable** (2-3 weeks):

- Queue page (next vehicles in)
- Run an inspection (form, defect entry, pass/fail)
- Recent inspections list with re-inspection link

**Full scope**: certificate issuance, fraud detection, defect analytics.

### 4.5 Priority 5 — Insurance Partner (`web-insurance`)

**Minimum viable** (1-2 weeks):

- Webhook delivery log (per-event success/failure)
- Policy lookup form
- Active claims list

**Full scope**: bulk policy upload, fraud analytics, claim adjudication.

### 4.6 Priority 6 — Police PWA depth

Already live; expand with:

- Offline mode (IndexedDB queue for fines issued without connectivity)
- QR/NFC verification of citizen-presented IDs
- ANPR-camera integration (already exists in `/scan`; needs a real model
  to back the OCR)
- Bodycam evidence upload chunked + hash-chained

---

## 5. Phase 4 — Regulation engine evolution (parallel, lower priority)

### 5.1 When to extract a service

**Not yet**. The current table-driven approach (`regulation_offences` +
`regulation_escalation`, read by `fines` and `registry`) is fine while
the rules are flat per-tenant. Extract a `naditos-regulations` service
when **any** of these are true:

- Three or more services need to read the rules (currently 2).
- Rules need versioning that survives tenant edits (audit trail of who
  changed what offence amount when).
- Rule evaluation needs to compose multiple sources (national + municipality
  + emergency decree) at runtime.
- Rules need to be queryable by external systems (court, parliament).

### 5.2 Service shape (when the time comes)

```
naditos-regulations
  GET   /v1/regulations/active                  current pack for tenant
  GET   /v1/regulations/packs                   listable, paginated
  GET   /v1/regulations/packs/{id}              manifest + signature
  POST  /v1/regulations/packs/{id}/sign         ministry-key signature
  POST  /v1/regulations/packs/{id}/apply        bind to tenant (admin)
  POST  /v1/regulations/evaluate                given (vehicle, context),
                                                return offences that fire
                                                and the resulting fine amount
```

Storage stays in Postgres. Service adds:

- **`regulation_audit`** — every read-of-rule + change-of-rule logged with
  the same hash-chain pattern as `audit_events`.
- **Detached signature** stored alongside the pack. The ministry key is the
  trust root; the service verifies on read.
- **Override hierarchy**: country pack → regional override → emergency decree
  (time-bounded, overrides on top).

### 5.3 Court integration shape

Out of scope for the regulation service. Court is a downstream **consumer**
of the audit + fines + regulation surfaces, not a peer service. Court
integration happens via:

- Read-only access to a curated subset of the audit + fines API.
- A `case_id` foreign key on `fines` and `audit_events` populated when a
  fine escalates to court.
- A "court read role" that bypasses certain RLS predicates.

---

## 6. Phase 5 — AI / Smart-City integration points (12+ months)

**Treat AI as a downstream consumer, not a service mesh.** The platform
should remain functional and auditable without any AI; AI augments,
never gates.

### 6.1 Integration patterns

| Use case | Pattern | Privacy implication |
|---|---|---|
| AI ANPR (better OCR) | Swap the model behind `anpr-gateway` or run a sidecar | Plate images stay in tenant region |
| Stolen-vehicle detection | Subscriber on the audit event stream filtering for ANPR scans matching a watchlist | All decisions logged in audit |
| Officer behavioural anomalies | Batch job over `audit_events`, writes flags to `audit_alerts` (already exists) | Computes locally; no PII leaves |
| Fake-document detection | Microservice the citizen portal calls before submitting a license renewal | Document never leaves tenant |
| Congestion analytics | Read-only consumer of ANPR scans; aggregated outputs only | Trip-level data anonymised |

### 6.2 Integration gates

Before any AI module ships:

1. The model and training data have a documented residency policy.
2. The model's outputs are **advisory**, never enforcement triggers.
3. Every model decision lands in `audit_events` with `service=ai-<name>`,
   so the compliance app can review them.
4. The model is versioned and rollback-able from the admin UI.

### 6.3 What we are NOT building in Phase 5

- LLM-driven policy authoring.
- Automated fine adjudication.
- Predictive policing.

These are political, legal, and ethical hot rails this platform should not
touch without explicit ministry-level review.

---

## 7. Phase 6 — Sovereign cloud readiness (12-24 months)

**Fly.io is transitional.** It's fast, cheap, and sufficient for pilot
deployments. It does not survive a sovereign-deployment requirement that
specifies on-prem datacentres, Kubernetes-native orchestration, or
air-gapped operation.

### 7.1 Migration path (Fly → K8s)

| Today (Fly) | Sovereign (K8s) |
|---|---|
| `fly.toml` per service | `Deployment` + `Service` + `HorizontalPodAutoscaler` per service |
| Fly secrets | Sealed-secrets / Vault |
| Fly 6PN private networking | Cilium / NetworkPolicy + service mesh (Istio or Linkerd) |
| Fly Postgres | Patroni-managed Postgres on local volumes |
| Fly proxy | NGINX ingress / cloud LB |

The Go service code does not change. The Dockerfile already produces a
distroless static binary that runs on K8s. The config layer
(`packages/go-common/config`) reads env vars regardless of platform.

### 7.2 What needs to change

- `deploy/k8s/` already has stub manifests; bring it to feature parity
  with `deploy/fly/`.
- A migration runner that can apply schema changes to a new region's
  Postgres without needing a `fly proxy` tunnel.
- A per-region routing layer if the sovereign deploy is multi-region.

### 7.3 Air-gap considerations

- All container images pullable from a private registry.
- Every external dependency vendored or mirrored (Go modules, npm packages,
  Loki/Prom binaries).
- Every secret-management interaction works without internet.
- A "country pack import" file format that doesn't require online ministry
  signing (offline signature verification only).

---

## 8. Cross-cutting: security hardening

### 8.1 RBAC

**Current**: `admin` / `officer` / `citizen` / `court` are seeded; `inspector` /
`auditor` / `insurance_partner` are missing despite their consoles being
scaffolded.

**Action**: add a migration that seeds the missing roles and their permission
grants per tenant pack, then add `NeedsRole` gates at both gateway and
service layers.

### 8.2 Audit immutability

`audit_events` already has `BEFORE UPDATE` and `BEFORE DELETE` triggers
that raise an exception (SQL in `0001_init.up.sql:412`).

**Verify** that triggers survive every future migration; add a migration
test that asserts they do.

### 8.3 Secret management

| Today | Sovereign |
|---|---|
| Fly secrets (per-app, opaque) | Vault / Sealed-secrets / cloud KMS |
| `JWT_SECRET` and `ADMIN_BOOTSTRAP_KEY` set once, rotated manually | KMS-backed rotation with grace window |
| `DATABASE_URL` includes the password | mTLS-authenticated DB connection, no shared secret |

**Phase 1 must**: document rotation procedures for the two secrets we have.
**Phase 6 should**: replace shared secrets with mTLS where possible.

### 8.4 Rate limiting

`gateway` has per-tenant per-prefix token-bucket rate limiting in
`services/gateway/internal/proxy/ratelimit.go`. Limits configured in
`routes.go`.

**Action**: surface limits in the admin UI; let ministries adjust without
redeploys.

### 8.5 Anti-corruption safeguards

The audit hash-chain is the technical anchor. The behavioural surface
(audit-events review, anomaly detection rules) is what makes it useful.
**Phase 3** builds out the compliance app to make this surface real.

### 8.6 Evidence integrity

`fine_evidence` already stores `sha256` and `bytes`. Object storage upload
is **not yet implemented** — evidence currently references an `s3_key` that
no service uploads to.

**Action** in Phase 3: wire the police PWA + admin to upload to S3-compatible
storage with the `sha256` recorded server-side, not client-claimed.

### 8.7 Tamper detection

Beyond the audit hash chain:

- `evidence_sealed` table (in 0007) records the seal hash.
- A nightly job (not yet implemented) re-verifies a sample of evidence
  blobs against their stored hashes.
- A nightly job (not yet implemented) re-runs `audit_verify` and pages
  on any break.

---

## 9. Cross-cutting: deployment & CI/CD

### 9.1 Environments

| Environment | Purpose | Where |
|---|---|---|
| **dev** | Local-only, `docker compose up`, ephemeral | Engineer's laptop |
| **staging** | CI target; matches prod topology | A second Fly org or namespace |
| **prod** | The live gateway (`naditos-gateway.fly.dev` today) | Sovereign in Phase 6 |

Today there's only prod. Phase 1 introduces staging.

### 9.2 Branch strategy

- `main` — protected, merge-only via PR, all CI green.
- `claude/*` and `feat/*` — short-lived feature branches.
- `release/x.y` — long-lived for sovereign-deploy customers (Phase 6+).

### 9.3 CI pipeline (GitHub Actions, Phase 1)

```yaml
on: { pull_request: { branches: [main] } }
jobs:
  go-test:
    strategy: { matrix: { service: [auth, registry, license, fines, audit, anpr-gateway, insurance, inspection, notifications, gateway] } }
    steps:
      - go test ./...
      - go vet ./...
      - golangci-lint run
  go-common-test:
    steps: [ go test ./... in packages/go-common ]
  app-build:
    strategy: { matrix: { app: [police-pwa, web-admin, web-citizen, web-inspection, web-insurance, web-compliance] } }
    steps:
      - pnpm install --frozen-lockfile
      - pnpm --filter @naditos/<app> build
  migration-check:
    steps:
      - pg up + run all .up.sql in order
      - run all .down.sql in reverse, verify clean
```

### 9.4 Deploy pipeline

- Auto-deploy to staging on merge to `main`.
- Manual gate to prod, requiring two reviewers when sovereign deploys exist.
- Database migrations gated by a separate manual step until a migration
  runner with rollback proof exists.

### 9.5 Country-pack rollout

Country packs are *content*, not code. They roll out via:

1. Migration adds `country_packs` row + `tenant_country_pack` mapping.
2. `naditos-regulations` (when extracted) re-reads on next request.
3. No service redeploy needed — verified by 0012 (Norway).

---

## 10. Cross-cutting: data & multi-tenancy

### 10.1 Tenant onboarding flow (target state)

A new ministry / country should onboard in <30 minutes:

1. Ministry signs a country pack manifest (offline ceremony).
2. Operator runs `bash scripts/onboard-tenant.sh <country_code> <manifest.json>`
   which:
   - Inserts the country pack
   - Inserts the tenant row with the right plate_regex / currency / locale
   - Seeds the role taxonomy
   - Mirrors the demerit + retention policies
   - Creates an initial admin user with a one-time-use password
   - Outputs the credentials for secure handoff
3. Ministry admin logs in, rotates the password, generates more users
   from the admin UI.

**Today** this is a hand-rolled SQL migration (`0012_country_pack_no.up.sql`).
The script doesn't exist yet — Phase 3 action.

### 10.2 Database evolution path

| Stage | Topology | Tenant count |
|---|---|---|
| **Now** | Single `naditos` DB, RLS-isolated tenants | 1-3 |
| **Pilot** (Phase 2) | Single DB, weekly `pg_dump` per-tenant for compliance | 5-10 |
| **Scale** (Phase 4) | Per-region DB cluster, tenant routing at the gateway | 20-50 |
| **Sovereign** (Phase 6) | Per-country dedicated cluster, country pack federation | 100+ |

The transition from "single DB" to "per-region" is mechanically a tenant
routing change at the gateway plus a per-tenant `DATABASE_URL` override
on the services. The data model doesn't change.

### 10.3 PII boundaries

Today `users.national_id`, `owners.national_id`, `fine_evidence`, and
biometric fields (when added) are PII. They live in the same DB as
operational data.

**Phase 6** isolates PII into a separate "personal data vault" Postgres
with stricter access controls; references are by UUID, joins happen in
the application layer with audit events for every join.

---

## 11. Risk register (top 10)

| # | Risk | Impact | Likelihood | Phase | Status |
|---|---|---|---|---|---|
| 1 | PG cold-start locks the stack | High | Always | Phase 1 task #1 | Open |
| 2 | CI runs but isn't required for merge ⇒ regressions can ship | Medium | High | Phase 1 task #2 (see docs/BRANCH-PROTECTION.md) | Open |
| 3 | `JWT_SECRET` rotation procedure missing | High | Medium | Phase 1 task #5 | Open |
| 4 | Stale binary deploys (build cache) | Medium | Medium | Phase 1 task #7 | Mitigated (--no-cache) |
| 5 | Single Postgres = single blast radius | High | Low | Phase 6 | Accepted for now |
| 6 | Audit triggers could be dropped by a future migration | High | Low | Phase 8 hardening | Accepted |
| 7 | Vercel-only frontend = vendor lock | Low | High | Phase 6 (port to K8s ingress) | Accepted |
| 8 | `ADMIN_BOOTSTRAP_KEY` left enabled forever | Medium | Medium | Phase 1 task #6 | Open |
| 9 | Inspection/insurance/compliance shells unrolepgated | Low | High | Phase 3 #8 | Open |
| 10 | No staging environment | Medium | High | Phase 1 task | Open |

---

## 12. This-week and this-month checklists

### This week (engineering)

- [ ] Move `naditos-pg` off auto-stop (`min_machines_running=1` or migrate to MPG)
- [x] CI workflow exists (`.github/workflows/ci.yml`) — go build/vet, integration, smoke, govulncheck, pnpm build
- [x] `make check` runs the same gating locally (this PR)
- [ ] **Enable branch protection on `main`** (one-time UI step — see `docs/BRANCH-PROTECTION.md`)
- [ ] Stamp git sha into every service's boot log
- [ ] Document `JWT_SECRET` rotation in `deploy/fly/README.md`

### This month (engineering)

- [ ] Stand up staging Fly org
- [ ] Deploy the four observability apps (Loki, Prometheus, Tempo, Grafana)
- [ ] Add OTel context propagation to gateway → upstream calls
- [ ] Build Ministry Command "Officer activity" + "Disputes queue" pages
- [ ] Build Citizen Portal "Pay fine" + "View my vehicles" pages
- [ ] Add `inspector` / `auditor` / `insurance_partner` roles + role gates

### This quarter (strategic)

- [ ] First non-demo country onboarding (formalise the script)
- [ ] Evidence upload to S3-compatible storage with server-computed sha256
- [ ] First SLO published with a real-time dashboard
- [ ] Disaster-recovery dry run: kill the primary PG, recover from backup, time it
- [ ] First sovereign-deploy partner conversation

---

## 13. What this document isn't

- **Not a contract**: dates are aspirational; ministry-driven discovery
  may reorder phases.
- **Not a substitute for product discovery**: every "what to build" item
  in Phase 3 should be validated with a real ministry user before being
  built.
- **Not a freeze**: this doc updates with every meaningful architectural
  decision. Edits go through PR review like any other change.
- **Not a sales document**: it intentionally calls out what doesn't exist
  yet. Sovereign-deployment partners should see the gaps before they
  contract.

---

## Appendix A — Glossary

| Term | Meaning |
|---|---|
| Country pack | Versioned manifest of offences, escalation rules, plate format, currency, and locale specific to a jurisdiction. Stored in `country_packs` + bound via `tenant_country_pack`. |
| Tenant | A jurisdiction (`demo`, `no`, eventually `de`, `ke`, …). Every domain row carries `tenant_id`; RLS enforces isolation. |
| Officer | A user with role `officer`. Issues fines, runs ANPR scans, looks up vehicles. |
| Audit event | An append-only, hash-chained record of an operationally significant action. |
| BYPASSRLS | Postgres role attribute that lets a service-level role read/write across tenant rows; used by the auth service and by `naditos_admin`. |
| 6PN | Fly's IPv6-based private network between apps in the same org. |

## Appendix B — Repository map

```
apps/                  Six Next.js 14 apps; each has its own vercel.json
  police-pwa           Officer-facing PWA (live)
  web-admin            Ministry Command (scaffolded + vehicle create live)
  web-citizen          Citizen self-service (scaffolded)
  web-inspection       Inspection Authority (scaffolded + home wired)
  web-insurance        Insurance Partner (scaffolded + home wired)
  web-compliance       Audit & Compliance (scaffolded + home wired)

services/              Ten Go microservices, each with cmd/server + internal/api
  gateway              Public reverse-proxy + JWT verify + rate-limit
  auth                 Login, refresh, /me, admin-create-user
  registry             Vehicles + owners + transfers
  license              Driver licenses
  fines                Fines, payments, disputes, escalation
  audit                Append-only ledger, alerts, officer stats
  anpr-gateway         ANPR ingestion + matching
  insurance            Provider integration, webhooks
  inspection           Inspection records, worker
  notifications        Outbound delivery (email/SMS, currently stubbed)

packages/
  go-common            Shared Go: config, db, auth, audit, httpx, server,
                       observability, regulation, testkit, events, logger
  web-common           Shared Next.js: api client, session, UI primitives,
                       layout, status helpers, i18n

db/migrations/         12 SQL migrations (.up.sql + .down.sql); idempotent
                       where possible (ON CONFLICT DO UPDATE / NOTHING)

deploy/
  fly/                 fly.<service>.toml per service + bootstrap.sh
  docker/              go-service.Dockerfile (multi-stage, distroless)
  k8s/                 Skeleton manifests (parity with fly/ is Phase 6)

scripts/               smoke.sh (local), seed.sh, migrate.sh

docs/                  ROADMAP.md (this doc) + DEPLOY.md
CLAUDE.md              Operator notes for Claude when working in this repo
```

## Appendix C — How to contribute to this roadmap

1. Open a PR against `docs/ROADMAP.md`.
2. PR description must include: which phase the change affects, what risk
   it raises or retires, and what's the smallest next action.
3. Roadmap PRs require the same review as code PRs once branch protection
   lands (Phase 1 task #2).
