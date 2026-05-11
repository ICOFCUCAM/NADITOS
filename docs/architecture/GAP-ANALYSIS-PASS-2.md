# NADITOS Sovereign-Grade Strategic Axes — Pass 2

> Inventory of NADITOS against the 18 sovereign-grade strategic axes from
> brief #2 that the Pass-1 review did not cover in depth, plus two
> cross-cutting checks (`tenant_id` flow, event-driven architecture).
> Companion to `GAP-ANALYSIS-PASS-1.md`. Same codebase walk (post Go 1.25
> merge).

## A. Multi-Country Sovereignty Architecture — PARTIAL

`tenants(id, country_code (ISO-3166-1), default_locale, currency, plate_regex)`
in `db/migrations/0001_init.up.sql:29-38`. `officer_profiles.jurisdiction`
is a flat nullable TEXT. `tenants.modules` JSONB for selective feature
activation.

**Missing:** regions / municipalities / agencies hierarchy. No per-tenant
DB config or region-pinning — all services share a single `DATABASE_URL`.
No data-residency hooks.

## B. Global Regulation Engine — PARTIAL

`regulation_offences(name JSONB)` with locale keys
(`db/migrations/0001_init.up.sql:109-119`). `regulation_escalation`
per-tenant (lines 121-128). `country_packs(version, effective_from,
superseded_by, manifest JSONB, signature BYTEA)`. `tenant_country_pack`
applies a pack to a tenant.

**Missing:** regulation simulator, version-diff/compare API,
municipality-level overrides, effective-date scheduling job (no
materialised "current pack" per date), audit of regulation changes over
time, documented manifest schema.

## C. Multi-Language & Cultural Localization — PARTIAL

`packages/web-common/src/i18n.ts` is a minimal factory: detects locale
from `localStorage` / navigator / fallback `"en"`. RTL flag for `["ar"]`.
`Locale` type: `"en" | "fr" | "de" | "es" | "no" | "ar"`. JSONB locale
keys on `regulation_offences.name`. `tenants.default_locale`.

**Missing:** no full i18n framework (next-intl / react-intl /
next-i18next). No `users.locale`. No RTL CSS scaffold. No locale-aware
date/currency formatters. Pluralization not automated.

## D. Offline-First National Operations — MISSING

No service-worker scaffolding in `apps/police-pwa`. No IndexedDB
mutation queue. UI shows a "Camera offline" indicator in
`apps/police-pwa/app/(app)/scan/page.tsx` but no sync mechanism.

> Conflict with Pass-1: Pass-1 stated police-pwa caches last 14 days of
> plate status in encrypted IndexedDB and queues fine drafts locally;
> Pass-2 found no service-worker code. **Treat offline-first as
> effectively MISSING until a runtime verification confirms otherwise.**

**Missing:** service worker, draft fine persistence, mutation queue,
sync-on-reconnect, conflict resolution, per-action sync state UI.

## E. International Vehicle Interoperability — MISSING

`anpr_scans.source` enum includes `'border'` (scaffolding only). No
cross-border verification API, no INTERPOL/Red Notice integration, no
external-country adapter scaffolding.

## F. Digital National Transport Identity — MISSING

No QR/NFC, no `.pkpass` templates, no W3C VC / DID support, no signed-
payload endpoints. Driver licenses table exists but no digital-proof
issuance endpoints. Would require a PKI / key-management substrate
first.

## G. AI-Assisted Governance Foundation — PARTIAL

`officer_daily_stats.anomaly_score` (REAL) populated nightly by
`services/audit/internal/rollup/rollup.go` (line 50 hardcoded 60-min
sweep). Two detectors: `officer_high_anomaly_z` (z > 2.0),
`officer_high_cancel_rate` (>30% with ≥5 fines).

**Missing:** per-tenant tuning table for thresholds, ticket-farming
detection (same officer, same location, <5min apart), suspicious-
override detection (admin cancels disputed fines), cross-officer
collusion patterns, ML pipeline (feature store / model serving /
retraining).

## H. Court & Justice Integration — PARTIAL

`packages/go-common/contracts/court/court.go` defines `FilePacket`,
`CaseRef(status: filed|scheduled|judged|dismissed)`, `Provider`
interface. DevStub only. `fines.status='court'` flag.
`regulation_escalation.action='court'`.

**Missing:** `court_cases` table, warrant integration / lookups, court
summons workflow, evidence-export formats beyond `FilePacket.EvidenceURLs`,
legal-hold / preservation orders, judge-override audit trail.

## I. Global Digital Payment Infrastructure — PARTIAL

`fine_payments(method: card|mobile|treasury, provider_ref, status:
pending|succeeded|failed|refunded)`.
`packages/go-common/contracts/payments/payments.go` defines `Intent`,
`Money{amount string, currency}`, `CreateIntent/GetIntent/Refund/
VerifyWebhook`. DevStub only. Wired at
`services/fines/cmd/server/main.go:38`.

**Missing:** real adapters (Stripe, Adyen, M-Pesa, MTN MoMo,
Flutterwave). No `payment_providers` routing table, no
`payment_methods` for stored instruments, no multi-currency FX, no
bank-API / treasury implementations.

## J. Transport Economy & Logistics Intelligence — MISSING

No fleet/freight/road-usage/taxation tables. `vehicles.category` exists
but no aggregation queries.

## K. Smart City Integration — MISSING

`anpr_scans.source` enum includes `'toll'` and `'border'` (scaffolding
only). No IoT/MQTT, no parking/toll/congestion/traffic-signal
integration.

## L. Emergency Response Integration — MISSING

No emergency / dispatch / 112-911 integration.

## M. Public Transport Governance — PARTIAL

`vehicles.category` (TEXT, no enum constraint) admits buses/taxis. No
`route_licenses`, `operator_permits`, or public-transport-specific
offences.

## N. Customs & Border Control — MISSING

`anpr_scans.source='border'` is the only hook. No customs workflow,
transit permits, or import/export ledger. **Pass-1's mention of
`vehicles.import/export_records` is incorrect — those columns do not
exist.**

## O. National Transport Data Exchange APIs — MISSING

No `api_clients`, `api_keys` (separate from internal S2S), `webhooks_out`,
or external-consumer API gateway routing. `services/gateway` is an
internal reverse proxy only — no OAuth2 / client-credentials flow.

## P. Sovereign Cloud & National Hosting Portability — PARTIAL

`deploy/k8s/`: `00-namespace.yaml`, `10-config.yaml` (ConfigMap +
Secret placeholder), `30-services.yaml` (Deployments + Services + HPAs
for all microservices), `40-gateway.yaml` (nginx Ingress). `deploy/fly/`
has per-service TOML. **No Helm charts, no Kustomize overlays, no
per-environment overlays.** Storage contract is cleanly adapter-based;
PostgreSQL connection is `DATABASE_URL`-only. No air-gap / vendor-all
flag.

## Q. Digital Twin & Simulation Readiness — MISSING

No road network model / scenarios / simulator. Geospatial columns exist
(`anpr_scans.geo_lat/lng`, `fines.geo_lat/lng`, `anpr_jobs.geo_lat/lng`)
but no PostGIS, no spatial indices.

## R. Anti-Corruption Intelligence (sovereign-grade) — PARTIAL

`audit_alerts(kind, subject_kind, subject_id, day, severity, details
JSONB, resolved_at/by)`. Two anomaly types as in G. Static thresholds
in `services/audit/internal/rollup/rollup.go` lines 240-244. UI in
`services/audit/internal/api/alerts.go` lists open alerts + resolution
form. No drill-down into rule logic, no decision-path audit, no
ticket-farming / suspicious-override / collusion detection.

---

## S. Cross-Cutting: tenant_id Flow — VERIFIED

All 39 core domain tables carry `tenant_id NOT NULL REFERENCES
tenants(id) ON DELETE CASCADE` with RLS `tenant_isolation` policy
(`USING (tenant_id = app_tenant()) WITH CHECK (tenant_id = app_tenant())`).

Tables intentionally **without** `tenant_id`:
- `event_consumer_offsets` (global consumer position)
- `country_packs` (shared regulation bundles)
- `tenant_country_pack` (the mapping itself)

Session vars `SET LOCAL app.tenant_id / app.user_id / app.role` are set
by auth middleware. The connecting DB user is BYPASSRLS so background
workers can act cross-tenant.

## T. Cross-Cutting: Event-Driven Architecture — PARTIAL

Producers `INSERT INTO event_outbox(tenant_id, envelope JSONB)` within
their domain transaction. `packages/go-common/events/consumer.go`
relays unfilled rows to a transport bus, marks `delivered_at`.
Subscribers track `event_consumer_offsets(consumer_name, last_event_id)`.

Transport: **NATS** in production (`events.OpenPublisher(NATS_URL)`),
**in-process** in dev (`events.InProc`).

**Missing:** Kafka/RabbitMQ adapters, dead-letter queue, replay/rebuild
API, schema registry, event versioning (envelopes are free-form JSONB).

---

## Status Summary

| Axis | Status |
|---|---|
| A. Multi-Country Sovereignty | PARTIAL |
| B. Global Regulation Engine | PARTIAL |
| C. Localization | PARTIAL |
| D. Offline-First Operations | MISSING (verify Pass-1 claim) |
| E. Int'l Vehicle Interop | MISSING |
| F. Digital Identity / Wallets | MISSING |
| G. AI-Assisted Governance | PARTIAL |
| H. Court & Justice | PARTIAL |
| I. Payment Infrastructure | PARTIAL |
| J. Transport Economy / Logistics | MISSING |
| K. Smart City Integration | MISSING |
| L. Emergency Response | MISSING |
| M. Public Transport Governance | PARTIAL |
| N. Customs & Border | MISSING |
| O. Data Exchange APIs | MISSING |
| P. Sovereign Cloud Portability | PARTIAL |
| Q. Digital Twin / Simulation | MISSING |
| R. Anti-Corruption Intelligence | PARTIAL |
| S. tenant_id flow | EXISTS |
| T. Event-driven architecture | PARTIAL |

## Most Dangerous Absences

1. **Offline sync queue (D)** — claimed by Pass-1, not found by Pass-2.
   If genuinely absent, police-pwa loses field-issued fines in poor
   connectivity.
2. **Cross-officer collusion detection (R)** — current anti-corruption
   work is per-officer only.
3. **Customs/border (N)** — only an ANPR enum, no enforcement logic.
4. **No external/partner API governance (O)** — third-party
   interoperability isn't gated at all.
5. **No regional hierarchy within a tenant (A)** — blocks any
   country whose enforcement is municipal.
