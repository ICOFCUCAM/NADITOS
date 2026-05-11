# NADITOS Unified Roadmap

> Synthesises `GAP-ANALYSIS-PASS-1.md` (18 foundational areas),
> `GAP-ANALYSIS-PASS-2.md` (18 sovereign-grade axes + cross-cutting),
> and `CAMERA-PLATFORM.md` into one ordered execution plan.
>
> The plan obeys both strategic briefs: foundation before optimization,
> substrate before AI, coherence before scale. Tracks are roughly
> ordered by "what does the next track depend on?" — tracks within the
> same tier can run in parallel.

---

## 8 sub-roadmap views (filter index)

The strategic brief asked for 8 named roadmaps. Rather than duplicate
content across 8 files (which invites drift), each is a *view* into
this unified roadmap — a filter over the tracks below.

| Sub-roadmap | Tracks that compose it |
|---|---|
| Sovereign architecture | 1A · 3A · 3C · 3I |
| Multinational scaling | 1A · 1B · 3A · 3E · 3H · 3I |
| Observability | 1C · 1H · 2B · 2G |
| Deployment hardening | 0 · 1E · 1H · 3I |
| Frontend maturity | 1B · 2A · 2B · 2C · 2D · 2E · 2F · 2G · 2I |
| Interoperability | 3C · 3F · 3H · 3K |
| Anti-corruption intelligence | 1D · 2G · 4 |
| Long-term infrastructure | every track, sequenced |

---

## Operating principles (apply to every track)

1. **No new microservices** unless an existing one cannot be reasonably
   extended. We have 10 services; consolidation before proliferation.
2. **No schema redesigns**. Migrations are additive only. RLS +
   `tenant_id` patterns are sacred.
3. **No UI rewrites**. Add real screens to the existing shells; do not
   rebuild apps.
4. **No "AI everywhere"**. AI is a layer on top of audit/event data
   that must already be coherent and complete.
5. **Every track has a stop condition**. If a track is not done by its
   exit criteria, we do not start its dependents.
6. **Evidence trumps narrative.** When Pass-N gap analyses disagree, a
   runtime check decides. See §0 of `CAMERA-PLATFORM.md`.

---

## Track 0 — Stop the bleeding (≤ 2 weeks)

Existing holes that produce wrong, lost, or undetected data **today**.
Nothing in this track is a feature; it is all bug-class.

| Item | Why it's a bleed | Source |
|---|---|---|
| Photo bytes never reach storage | Server-recorded SHA matches nothing; "court-admissible" is hollow | `CAMERA-PLATFORM.md` §0.2 |
| Login audit not wired | Brute-force / unauthorised access undetectable | Pass-1 #1 |
| Branch protection unenforced on `main` | Regressions ship; we already saw one | Pass-1 #18 |
| Postgres auto-stop on Fly | 20–40s cold-start fails health checks intermittently | Pass-1 urgent #1 |
| Smoke test marked `continue-on-error` | Hides regressions; PR #11 inherits the problem | Pass-1 #18 |
| Pass-1↔Pass-2 disagreements | "Offline-first" claim was fiction | `CAMERA-PLATFORM.md` §0.1 |

**Exit criteria**

- `POST /v1/evidence/presign` exists and is the only path for evidence
  ingestion. PWA uploads original bytes, server HEADs the object and
  verifies SHA before accepting the `fine_evidence` row.
- `auth.handleLogin()` emits `audit.Emit(ctx, "auth.login", ...)` for
  both success and failure with IP + UA.
- Branch protection re-enabled on `main`: all required checks (excl.
  smoke) green, ≥1 approving review, dismiss-stale on push. (If
  approvals-of-own-PR remains a workflow constraint, document the
  bypass-with-justification path.)
- Postgres on Fly: managed PG, or `min_machines_running ≥ 1` on the
  PG VM. Cold-start failures eliminated.
- Smoke: root-caused (services-don't-start / empty logs). Once green,
  removed from `continue-on-error` and added to required checks.

**Out of scope here:** any new feature, any UI polish, any provider
adapter beyond what the evidence-presign endpoint needs.

---

## Track 1 — Substrate consolidation (4–8 weeks, parallelisable)

These items are prerequisites for every later track. Order within the
track is flexible; nothing here depends on Tracks 2+.

### 1A — Jurisdiction hierarchy

- Add `regions`, `municipalities`, `agencies` tables under `tenants`.
  All carry `tenant_id` + RLS.
- Migrate `officer_profiles.jurisdiction` from a flat TEXT to a
  foreign key (`agency_id`).
- Permission strings extend: `fines:create@municipality(X)`,
  `audit:read@region(Y)`. Middleware rejects writes outside an
  officer's jurisdiction.
- **Why first:** every country whose enforcement is municipal needs
  this before any real rollout. Pass-2 §A.

### 1B — Real i18n framework

- Adopt `next-intl` across `web-admin`, `web-citizen`, `web-inspection`,
  `web-insurance`, `web-compliance`, `police-pwa`.
- Add `users.locale` TEXT NOT NULL with fallback to
  `tenants.default_locale`.
- RTL CSS scaffold (logical properties, `dir` attribute wiring).
- Locale-aware date/currency formatters via Intl APIs.
- Translation files for `en` first; placeholder structure for
  `fr / es / no / ar / pt` so adding a language doesn't require code.
- **Why first:** Pass-2 §C. Citizen and ministry UIs cannot be
  translated retroactively without rewriting components.

### 1C — Observability stack deployed

- Deploy Prometheus + Grafana + Loki (or hosted equivalent).
- Wire OpenTelemetry SDK across services with OTLP exporter; spans
  cross service boundaries via the existing trace headers.
- Sentry (or equivalent) for unhandled errors in all services and
  web apps.
- Readiness probes check DB connectivity (not just process liveness).
- Three baseline dashboards: service-level health, DB pool
  saturation, audit-event ingestion rate.
- **Why first:** Pass-1 #16. Every subsequent track will produce
  incidents; we need to see them.

### 1D — Rate limiting + API throttling

- Per-IP + per-user + per-tenant limits at the gateway.
- Per-endpoint hot-path limits (login, presign, evidence ingest,
  fine submission).
- Per-officer fine-issuance budget surfaced as a tenant-configurable
  policy (anti-fine-farming substrate).
- **Why first:** Pass-1 #17. Required before any external API access.

### 1E — Staging environment

- Duplicate Fly org as `naditos-staging`.
- CI deploys PR branches to staging; production deploy is a manual
  promotion from a green main with a smoke pass.
- Migration rollback test in CI: apply N → roll back → re-apply.
- **Why first:** Pass-1 #18. Tracks 2+ are too large to ship straight
  to prod.

### 1F — Evidence-platform substrate

This is **Camera Platform Phase C1** from `CAMERA-PLATFORM.md`. Listed
here because it is foundational rather than camera-specific:

- Presigned PUT endpoint, server-side SHA verify (already in Track 0
  exit criteria — Track 1F is the productionisation: encryption,
  per-tenant buckets, retention enforcement actually running).
- Offline queue + encrypted IDB in police-pwa with the explicit state
  machine in `CAMERA-PLATFORM.md` §4.
- `evidence_custody` events on every action.
- Exit test: kill the tab mid-flow, prove bytes are durable + hash
  matches.

### 1G — Operational hygiene

- MFA enrolment + verification endpoints (`auth` service).
- Password complexity rules.
- API-key model for service-to-service (separate from JWT).
- Secret rotation runbook turned into automation (per-service
  re-deploy with new JWT signing key, grace period for old).

### 1H — Enterprise Operations Center

Beyond the observability *substrate* in 1C, the brief calls for an
**operations center**: not just metrics flowing, but a coordinated
view of running the platform.

- Incident-management workflow (PagerDuty-class or open equivalent):
  on-call rotations, escalation policies, post-incident review
  templates. Integrate Sentry / Alertmanager as sources.
- Deployment-visibility dashboard: which service, which version,
  which environment, deployed by whom, when. Links to CI run + diff.
- Audit-operations visibility: live view of audit-event ingestion
  rate per service, audit-chain integrity status, alert backlog,
  unresolved `audit_alerts` queue. Pairs with Track 2G forensic UI.
- Service-level objectives (SLOs) defined per service with error-
  budget burn-down dashboards.
- Operational runbooks linked from alerts (every alert has a "what
  to do" doc in the repo).

**Why a separate track from 1C:** 1C is plumbing (collect signals).
1H is the workflow that turns signals into action. Both ship in
Track 1 because Track 2+ work cannot be operated safely without
incident management.

**Track 1 exit criteria**

- Jurisdiction hierarchy migrated and used by at least one service
  (`fines`).
- i18n live in `en` across all 6 frontends.
- Grafana shows live metrics from every Go service, every web app
  reports errors to Sentry.
- Rate limiting in place at the gateway; integration test confirms
  per-tenant cap fires.
- Staging environment deploys on PR; migrations rollback-tested.
- Evidence pipeline durable end-to-end.

---

## Track 2 — Feature completeness for existing capabilities (8–16 weeks)

Now we close the "EXISTS / PARTIAL" gaps from Pass-1 without inventing
new capability classes. This is what turns the MVP into a usable
platform.

### 2A — Citizen self-service

- License renewal request + status tracking.
- Full fine-payment flow (method selection, real payment provider,
  receipt PDF, refund visibility).
- Inspection booking (depends on Track 2C).
- Transfer accept-flow + re-send/extend codes.
- Document wallet (registration certs, insurance proofs, inspection
  certs uploaded by citizens — flows through the evidence pipeline
  from Track 1F).
- Notifications inbox wired to the notifications service.
- Whistleblower / corruption-report submission.

### 2B — Ministry command (web-admin)

- National KPIs dashboard (fines issued, revenue, vehicle count,
  license suspensions, enforcement coverage).
- ANPR scan browser/search with geo-filters.
- Officer management (badges, jurisdiction, device binding,
  suspension).
- Dispute queue with SLA timers.
- Provider-health dashboard (insurance, inspection, payments, ANPR).
- Anomaly explorer surfacing `audit_alerts` and
  `officer_daily_stats.anomaly_score`.
- Heatmaps (ANPR hotspots, fine density) — pre-PostGIS, plain SQL
  aggregations are fine for now.

### 2C — Inspection completeness

- Scheduling (appointment slots, inspection-center registry).
- Defect recording (taxonomy, severity, photos via evidence pipeline).
- Garage accreditation lifecycle.
- Real provider adapter (one to start, e.g. TÜV-shape).
- Circuit-breaker on failed providers.
- **Inspection-station offline mode** (`apps/inspection-station`):
  same offline state machine as the police-pwa in Track 1F. Inspectors
  in rural / weak-connectivity centers must be able to record defects,
  capture evidence, and issue conditional/fail outcomes offline, with
  durable queue + sync-on-reconnect. Brief #4 ("offline-first national
  operations") explicitly names *both* police and inspection.

### 2D — Insurance completeness

- Insurer onboarding (insurer registry, API keys, SLA).
- Policy state machine (pending / active / lapsed / cancelled).
- Claims table + claim status + claim-to-fine linkage.
- Fraud-rule definitions (table-driven, not hardcoded).
- Real provider adapter (one to start).

### 2E — Driver licensing completeness

- License state machine enforced in the service (not just DB).
- Suspension enforcement at every lookup.
- Testing-center registry + result recording.
- Biometric enrolment/verification endpoints (template-only, no
  raw biometric data stored; verification proof recorded in audit).

### 2F — Fines engine completeness

- Installment plans (schedule, partial-payment tracking).
- Repeat-offender rules at fine level (per-offence, sliding window).
- Appeal-window automation + appeal-type taxonomy.
- Cancellation must record a structured reason (enum + free text),
  with audit emission.
- Cross-link anomaly detector + escalation worker so single-officer
  fine-farming patterns auto-flag during escalation.

### 2G — Audit completeness

- Anomaly worker shipped (the rollup job exists; verify it runs,
  add the missing detectors: ticket-farming, suspicious-override,
  cross-officer collusion).
- Forensic / investigator UI in `audit` web app: per-resource
  timeline, hash-chain verify button, export-to-court.
- `audit.Emit` reliability: retry queue when audit service is down,
  back-pressure if outbox grows.

### 2H — Court & justice integration v1

- `court_cases` table (linked to fines/disputes).
- Court summons workflow.
- Evidence-export-to-court bundle (signed PDF + frame manifest +
  custody trail).
- Legal-hold flag honoured by the retention reaper.
- **Warrant integration substrate**: `warrants` table (issuer,
  subject_kind, subject_id, kind: arrest|seizure|stop, status, valid
  from/until, court_case_id). Officer scan flow surfaces warrant
  hits the same way it surfaces stolen-vehicle alerts. Service-to-
  court adapter is stubbed; real adapters land in Track 3 alongside
  partner-by-partner integration work.
- **Judicial review pipeline substrate**: `judicial_reviews` table
  (court_case_id, requested_at, requested_by, reason, status, decided
  at, decision). Surfaces in the citizen appeal flow when the
  administrative dispute path is exhausted. Linked to legal-hold so
  evidence cannot be reaped while a review is open.
- **Appeals workflow taxonomy**: replace the current `fine_disputes.
  status` free-string set with a typed enum (administrative |
  judicial | constitutional) plus per-tenant configuration of which
  paths are available.

### 2I — Camera Platform C2 (hotlist overlay)

From `CAMERA-PLATFORM.md`: tactical low-distraction overlay,
three-tier severity, haptic on CRITICAL. Pure UI layer over the
already-existing `GET /v1/vehicles/by-plate/{plate}`.

**Track 2 exit criteria**

- Each frontend (citizen, admin, inspection, insurance, compliance,
  police-pwa) does at least one full real-world workflow end-to-end.
- Real provider adapters wired in inspection, insurance, payments,
  notifications (one provider each minimum).
- A fine can flow from issuance → unpaid → escalated → disputed →
  court-filed → resolved, with full audit + evidence chain.

---

## Track 3 — Sovereign-grade axes (16–32 weeks, parallelisable)

Now the second strategic brief's axes that go beyond a "complete
single-country platform". Most depend on Track 1 jurisdiction +
Track 2 feature completeness; some are independent and can start
sooner.

### 3A — Regulation engine v2 (independent of much of Track 2)

- Municipality-level overrides (depends on Track 1A jurisdiction).
- Regulation simulator (apply pack X to dataset Y, project escalation
  outcomes).
- Version-diff API (`country_pack v3 vs v2`).
- Effective-date scheduling job (materialise "current pack" per
  date for time-travel queries).
- Audit trail of regulation changes over time.

### 3B — Digital national transport identity

- QR / NFC license generation, signed by ministry PKI.
- Verifiable Credentials (W3C VC Data Model).
- Apple / Google Wallet pass templates.
- Public-key infrastructure substrate (key management service or
  HSM-backed signer).

### 3C — Customs & border, international interop

- `customs_workflows`, `transit_permits`, `vehicle_import_export`
  tables.
- Cross-border stolen-vehicle federation (start with a single
  partner; design the adapter as one of many).
- INTERPOL adapter scaffolding (one-way query first).

### 3D — Public transport governance

- `route_licenses`, `operator_permits`, capacity audit, passenger
  manifest hooks.
- Public-transport-specific offences (overcrowding, fare evasion,
  unsafe loading) in regulation packs.

### 3E — Payment infrastructure depth

- `payment_providers` routing table + per-region fallback.
- Real adapters: Stripe, Adyen, M-Pesa, MTN MoMo, Flutterwave,
  national treasury.
- Multi-currency FX with daily-rate snapshot table.
- `payment_methods` for stored citizen instruments (PCI scope: tokens
  only).

### 3F — Smart city, IoT, emergency response

- ANPR ingestion clients table (already scoped in Camera Platform
  C4 — productionised here).
- Toll, parking, traffic-signal event ingestion (read-only first).
- 112/911 emergency dispatch handoff.
- Evacuation coordination workflow stubs.

### 3G — Logistics / transport economy

- Fleet schema, freight manifests, road-usage analytics.
- Taxation snapshot table (tax paid vs road usage).
- **Emissions intelligence**: `vehicles.emission_class` already
  exists. Add an emissions ledger that joins emission_class +
  vehicle category + road-usage analytics into per-region emissions
  estimates. Pairs with the brief's *environmental monitoring*
  requirement (smart city #12). Foundation for low-emission-zone
  enforcement, congestion-charge differentiation, and policy-impact
  analytics in Track 4.

### 3H — External API governance

- `api_clients` (OAuth2 client registration), `api_keys` (separate
  from internal S2S), `webhooks_out`.
- External-consumer gateway routing (distinct from the internal
  reverse-proxy gateway service).
- Rate limits per partner per scope.

### 3I — Multi-cloud portability

- Helm charts + Kustomize overlays for `dev / staging / prod`.
- Cloud-provider abstraction for the things still hardcoded
  (DATABASE_URL templating, NATS_URL).
- Air-gap readiness review (vendor all deps, no internet-required
  paths in the boot sequence).

### 3J — Digital twin / simulation foundation

- Add PostGIS extension; spatial indices on geo columns.
- Road-network model schema (intersections / links / segments).
- Defer the simulator engine itself to Track 4.

### 3K — Camera Platform C3 + C4

- C3: assistive on-device plate detector via TF.js/ONNX Web. Tiny
  model only. PWA hint, server authoritative.
- C4: multi-source ingestion (body-cam, dashcam, drone, fixed-cam,
  partner-webhook) via `anpr_ingest_clients`.

**Track 3 exit criteria**

- At least one sovereign-grade axis fully landed end-to-end per
  category (regulation v2, digital identity, customs, public
  transport, payments depth, smart city ingestion, logistics,
  external API gov, multi-cloud, geospatial substrate, camera
  multi-source).
- Documented multi-country rollout playbook.

---

## Track 4 — Intelligence layer (24–40 weeks, runs alongside late Track 3)

The brief's AI-foundation work. Cannot start meaningfully until the
audit chain, event-driven plumbing, and `model_registry` /
`model_runs` substrate are in place.

- ML pipeline foundation: feature store, training pipeline, model
  serving, monitoring (drift, fairness).
- Anti-corruption v2: ticket-farming detector, suspicious-override
  detector, cross-officer collusion graph analysis, decision-path
  audit (why did a rule fire?).
- Predictive analytics: accident-prediction, dangerous-corridor
  detection, fraud risk scoring on insurance/inspection.
- Digital-twin simulator engine.
- Field-intelligence overlays for the camera platform (suspicious
  convoy / repeat offender / route anomaly).

**Why this is last:** every detector needs months of clean audit
events from a coherent platform. Building detectors on a half-built
substrate produces false positives that erode trust.

---

## Track N — Native enforcement-camera companion

A parallel project, not a track in the main line. Out of scope for
this roadmap beyond ensuring the substrate (evidence chain, audit,
multi-source ingestion) can host it.

Kicked off when Tracks 1F + 2I + 3K are stable.

---

## Decisions blocking the start

These should be answered before Track 1 work begins; some have
appeared in earlier briefs and remain open.

1. **Multi-currency model.** Are amounts stored in minor units (long)
   or `numeric(20,4)`? Conversion table source? (Blocks Track 2A
   citizen payments + Track 3E payment depth.)
2. **PKI ownership.** Who holds the ministry signing keys for digital
   identity (Track 3B)? National HSM, cloud KMS, sovereign key
   ceremony? Affects substrate work in Track 1G.
3. **Evidence retention vs admissibility.** Are originally-captured
   frames retained at full fidelity for the entire retention window,
   or downsampled after N days? (Open in `CAMERA-PLATFORM.md` §14.)
4. **Enhancement admissibility.** Are server-side enhanced versions
   admissible as evidence in target jurisdictions, or only originals?
   Affects whether C3 needs a "raw-only capture" toggle.
5. **Attestation policy.** WebAuthn / Play Integrity for device
   binding — hard requirement, advisory, or per-tenant? Affects
   Track 1F evidence ingest.
6. **First two target countries.** The roadmap is country-agnostic,
   but ordering Track 3 work realistically needs to know whether
   country #2 is, say, a fellow EU country (close legal framework)
   or a sovereign neighbour with mobile-money-first payments. Affects
   priority within Track 3.

---

## What this roadmap deliberately does **not** include

- Aggressive UI refinement / animation work / brand polish. Per
  brief #1, deferred until foundations are coherent.
- Wholesale AI features baked into core workflows. Tracked in
  Track 4 only.
- Multi-country rollout. Not until Track 1 done + Track 2 enough to
  run a single country's enforcement end-to-end.
- Performance optimisation passes. Per brief #2, deferred until
  observability shows where to spend effort.
- Schema redesigns. Migrations are additive only.

---

## How to use this document

- Each track has named work items. Filing a PR should reference the
  track (`Track 0 / photo-upload-fix`).
- A track's exit criteria are non-negotiable before its dependents
  start.
- Open decisions live at the bottom; we update this doc when answered.
- Pass-N gap analyses are the *evidence base*; this roadmap is the
  *plan*. If reality diverges, gap analyses are re-run before the
  plan changes.
