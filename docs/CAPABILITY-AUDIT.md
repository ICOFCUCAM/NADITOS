# NADITOS — Foundational Capability Audit

> Pairs with `docs/ROADMAP.md` (strategic phasing). This document is the
> **status-of-the-foundation** snapshot. For each of the 18 mandatory
> capabilities of a national transport-governance platform, it records:
> what we already have, what's partial, what's missing, and the
> smallest disciplined next action.
>
> **Rule of this document**: every claim is grounded in a repo path or
> a migration number. If a row says ✓ but the path doesn't exist, the
> row is wrong — file a PR to correct it.
>
> Legend: ✓ = implemented · ◐ = partial · ✗ = gap

---

## How to read this

Each capability section follows the same shape:

1. **Intent** — one sentence on what the capability is *for* in a
   ministry context.
2. **Status table** — sub-capability × state × evidence/gap.
3. **Priority gaps** — the two or three smallest next actions that
   meaningfully raise the maturity bar.

A "gap" is not the same as "weak". Things marked ✗ are *not present*.
Things marked ◐ exist but need a named delta to be production-mature.

---

## 1. Identity & Access Governance

**Intent**: every actor's identity (citizen, officer, admin, court,
inspector, insurer, auditor) is provable, scoped, audited, and
revocable. No shared accounts. No off-the-record access.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| JWT auth | ✓ | `services/auth/internal/api/api.go::handleLogin`, `packages/go-common/auth/jwt.go` |
| Refresh tokens | ✓ | `refresh_tokens` table, `/v1/auth/refresh` |
| MFA readiness | ◐ | `users.mfa_secret` column exists (0001); no enrollment or challenge flow |
| RBAC | ✓ | `roles` / `role_permissions` / `user_roles`; gateway + per-service `NeedsRole` checks |
| ABAC readiness | ✗ | No attribute store; no policy engine; no precedent for non-role predicates |
| Tenant isolation | ✓ | RLS on every domain table with `app_tenant()` (0001:411–) |
| Organization / agency identity | ◐ | `officer_profiles.agency` is freeform text; no `agencies` table; no agency RBAC |
| Officer identity | ✓ | `officer_profiles` with badge + jurisdiction + device binding |
| Session management | ◐ | Refresh tokens stored, revocable; no "list active sessions / revoke this device" surface |
| Device tracking | ◐ | `refresh_tokens.device_id` column; not populated by login flow |
| Account recovery | ✗ | No email/SMS recovery flow |
| Password policies | ◐ | bcrypt cost 10 (`auth/password.go`); no length/complexity enforcement, no rotation |
| API key management | ✗ | No keys table; service-to-service uses shared `JWT_SECRET` instead |
| Service-to-service auth | ◐ | Same `JWT_SECRET` across services; OK at this scale, doesn't scale to mTLS |
| Emergency override workflows | ✗ | No break-glass flow; admin role is the de facto override |
| Account suspension | ◐ | `users.is_active` flag; admin UI can flip it; no audit-trail-on-flip surface |
| Role escalation approval | ✗ | Admin grants role directly; no two-person rule, no time-bounded grants |
| Login audit trails | ◐ | `login: success` slog line emitted (`api.go`); not yet written to `audit_events` |
| Security event tracking | ◐ | Bad-password, locked-out, suspicious-IP events not yet emitted to audit |

**Priority gaps**

1. Login + login-failure events to `audit_events` (`service=auth`,
   `action=login.success|login.failed`). One-day change, large
   anti-corruption payoff.
2. Agencies table + foreign key from `officer_profiles.agency_id`,
   freeform `agency` text deprecated. Lets the ministry app render
   per-agency analytics.
3. MFA enrollment for `role=admin` and `role=officer`. Schema column
   already there; TOTP server-side validate is ~80 LoC.

---

## 2. National Vehicle Registry

**Intent**: the single national source of truth for who owns what
vehicle, what state it's in, and what's permitted to be done with it.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Vehicle registration | ✓ | `POST /v1/vehicles`, `vehicles` table, `plate_regex` validation (PR #6) |
| Ownership history | ✓ | `vehicle_transfers` (0009); transfer events also land in `audit_events` |
| VIN / chassis tracking | ✓ | `vehicles.vin`, `vehicles.chassis_number` |
| Plate lifecycle management | ◐ | Plates immutable post-create; no decommission / re-issue flow |
| Import / export records | ✗ | No `vehicle_imports` table; no customs integration |
| Stolen vehicle tracking | ✓ | `is_stolen` + ANPR alert pipeline (0008) |
| Fleet ownership | ✗ | No `fleets` entity; commercial owners are individuals today |
| Commercial categorization | ◐ | `vehicles.category` is freeform; not gated by country pack |
| Tax linkage | ◐ | `tax_paid_through` column; no tax authority connector |
| Inspection linkage | ✓ | `inspection_records` + `inspection_expires_at` denorm |
| Insurance linkage | ✓ | `insurance_records` + `insurance_expires_at` denorm |
| Offense linkage | ✓ | `fines.vehicle_id` FK |
| Document uploads | ✗ | `fine_evidence.s3_key` exists; no service ever uploads. Same for vehicle documents |
| Digital certificates | ✗ | No certificate issuance |
| Immutable historical audit trail | ✓ | `audit_events` hash chain |
| Ownership transfer workflows | ✓ | Citizen-to-citizen via code; admin-mediated bulk path missing |
| Vehicle status lifecycle | ✓ | `v_vehicle_status` view (green/yellow/red/black) |

**Priority gaps**

1. Implement actual evidence upload to S3-compatible storage with
   server-computed sha256 (the `s3_key` field is currently a
   client-claimed string). Block "evidence chain" claims until done.
2. Agency-aware bulk transfer endpoint (`POST /v1/vehicles/{id}/transfer`
   with `target_owner_id`) for ministry-mediated transfers.
3. `vehicle_imports` table + customs read-only API contract.

---

## 3. Driver Licensing System

**Intent**: who is allowed to drive what, valid where, with what
endorsements.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Driver profiles | ✓ | `driver_licenses` table; `services/license` |
| Issuance workflows | ◐ | DB write path exists; no issuance UI; no biometric capture |
| Renewal workflows | ✗ | No renewal endpoint; citizen has nowhere to start renewal |
| Suspension / revocation | ✓ | `is_suspended` flag; `suspended_until`; demerit engine triggers |
| Penalty points | ✓ | `points` column + `driver_demerit_policy` (0004) |
| Biometric readiness | ✗ | No biometric storage column; no integration point |
| Provisional / full licenses | ✗ | No license-class progression; classes array is flat |
| Professional endorsements | ◐ | `services/license/endorsements` page in web-admin nav; no detail UI |
| Medical clearance | ✗ | No `medical_clearance` table |
| Testing center workflows | ✗ | No testing centers entity |
| QR-verifiable digital licenses | ◐ | `services/license/verify.go` returns license state; QR generation in police PWA `/verify` page is stubbed |

**Priority gaps**

1. Renewal endpoint `POST /v1/citizens/me/license/renew` — generates
   a pending renewal record, triggers payment, on payment success
   extends `expires_at`. Closes the citizen self-service loop.
2. Medical clearance table + linkage to renewal eligibility.
3. License-class progression policy in country pack (XX → A → B + age
   ladder).

---

## 4. Police Enforcement System

**Intent**: officer in the field can verify a vehicle, issue a
citation, and seal evidence — online or offline — with every action
recorded against their identity.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Roadside checks | ✓ | police-pwa `/scan` page |
| Citation issuance | ✓ | `POST /v1/fines` with evidence requirement (0001 fines table) |
| Geolocation logging | ✓ | `fines.geo_lat / geo_lng / geo_accuracy_m` |
| Evidence uploads | ✗ | `s3_key` not actually written by any uploader |
| ANPR integration | ✓ | `services/anpr-gateway` + `anpr_scans` table |
| Wanted vehicle checks | ✓ | `is_wanted` flag + status view |
| Seizure workflows | ◐ | `is_seized` flag; no workflow (custody record, release form, …) |
| Impound workflows | ✗ | No impound facility entity |
| Patrol activity logs | ◐ | `audit_events` captures actions; no `patrols` aggregate |
| Officer shift tracking | ✗ | No shift table; no "on duty" state |
| Incident reporting | ✗ | No `incidents` table; fines are not equivalent |
| Offline-first operation | ✗ | police-pwa has no IndexedDB queue; loses fines on bad connectivity |
| QR verification | ◐ | PWA `/verify` page exists; license-QR roundtrip not yet implemented |
| Evidence chain integrity | ✓ | `audit_events` hash chain + `evidence_sealed` (0007) |

**Priority gaps**

1. **Real evidence uploads.** Multipart upload to S3-compatible
   storage, server computes sha256, writes `fine_evidence` row.
2. Offline queue in police-pwa — IndexedDB persistent store of
   pending fines, drains on reconnect, deduplicated by client UUID.
3. Officer shifts (`officer_shifts` table) — required for both labor
   compliance and anti-corruption (correlating bribery rumours to
   on-duty periods).

---

## 5. Fines & Penalty Engine

**Intent**: enforce the country's regulation catalog consistently,
escalate predictably, integrate with payment and court without
ad-hoc code paths.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Configurable offense catalog | ✓ | `regulation_offences` + country pack manifest |
| Escalation rules | ✓ | `regulation_escalation` (5 stages, country-tunable) |
| Late penalties | ✓ | Multiplier in escalation table |
| Appeal workflows | ✓ | `fine_disputes` table; pending / accepted / rejected / court |
| Installment plans | ✗ | No partial-payment shape; `fine_payments` assumes single amount |
| Municipal overrides | ✗ | One regulation level per tenant; no nested municipality |
| Automatic suspension triggers | ◐ | Demerit engine suspends licenses when points threshold hit; no fine-amount-based suspension |
| Repeat offender logic | ✓ | `duplicate_window_min` per offense, dedup in fines handler |
| Court escalation readiness | ◐ | `case_id` not yet on `fines`; `fine_disputes.status='court'` exists as terminal label |
| Payment tracking | ✓ | `fine_payments` + webhook receiver with signature verify + idempotency |

**Priority gaps**

1. Installment plan shape: `fine_payment_plans` table (fine_id, n
   instalments, schedule), with `fine_payments` summed per-fine for
   the "paid" transition.
2. `case_id` foreign key on `fines` + `audit_events`, populated when
   escalation hits stage 5 (`court`).
3. Municipality regulation layer — a `regulation_overrides` table
   keyed on (tenant_id, region_code) that the lookup resolves before
   falling back to the country pack.

---

## 6. Inspection & Roadworthiness System

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Inspection scheduling | ✗ | No `inspection_appointments` table |
| Defect recording | ✗ | `inspection_records` has `result` (pass/fail/conditional) but no per-defect rows |
| Emissions checks | ◐ | `vehicles.emission_class` column; no measurement record |
| Pass/fail workflows | ✓ | `result` column |
| Garage accreditation | ✗ | No `inspection_stations` table |
| Inspector identity | ✗ | Inspectors aren't role-modeled |
| Fraud detection | ✗ | No "this station fails 0% of vehicles" anomaly rule |
| Certificate issuance | ◐ | `certificate_url` column; no generator |
| Recurring inspection rules | ◐ | Country pack manifest has `inspection_months` per vehicle category; not yet wired to expiry calculation |
| Heavy vehicle certification | ✗ | No heavy-vehicle category distinction beyond `category` text |

**Priority gaps**

1. `inspection_stations` table + `inspector_profiles` analogous to
   `officer_profiles`. Adds a role taxonomy entry.
2. `inspection_defects` join table for per-defect attribution; needed
   for fraud detection and analytics.
3. Inspection scheduling endpoint + the inspection PWA queue tile.

---

## 7. Insurance Federation System

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Insurer onboarding | ✗ | No `insurers` table; provider routing in `insurance` service is config-driven |
| Policy lifecycle | ✓ | `insurance_records` rows for each policy |
| Active policy verification | ✓ | `GET /v1/insurance/verify?plate=…` |
| Claims verification | ✗ | No `insurance_claims` table |
| Fraud flags | ✗ | No `is_suspicious` flag |
| Accident linkage | ✗ | No `accidents` table |
| Insurer APIs | ◐ | Webhook receiver (`POST /v1/insurance/webhooks/{provider}`) exists; bulk-policy push doesn't |
| Automated verification | ✓ | Async reconcile worker |
| Insurer audit trails | ✓ | `audit_events` for `service=insurance` |

**Priority gaps**

1. `insurers` table — first-class entity instead of config string.
2. `insurance_claims` table + claim status workflow (open / approved /
   denied / paid).
3. `POST /v1/insurance/policies/bulk` — atomic bulk push from
   insurer system, idempotent on policy_number.

---

## 8. National Audit & Compliance System

**Intent**: every operationally-significant action is observable to a
trusted reviewer, tamper-evident, and reviewable on court order — for
decades.

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Immutable audit trails | ✓ | `audit_events` with `BEFORE UPDATE / BEFORE DELETE` triggers raising exceptions (0001:412) |
| Access auditing | ◐ | Service-write paths emit audit; pure-read access not yet logged |
| Change tracking | ✓ | `before` / `after` JSONB columns |
| Suspicious activity detection | ◐ | z-score + cancel-rate detectors → `audit_alerts` (0008) |
| Anti-corruption analytics | ◐ | Officer daily stats (0003); no "officer cancels fines for same plate repeatedly" rule |
| Officer oversight | ◐ | `officer_daily_stats` + `audit/officers/me/stats`; no review queue in compliance app |
| Forensic timelines | ✗ | No "show me everything actor X did on day Y" query |
| Tamper detection | ✓ | Hash chain via `audit_verify` |
| Chain-of-custody support | ◐ | `evidence_sealed` ties evidence to seal hash; no transfer-of-custody record |
| Exportable legal reports | ✗ | No "produce a sealed PDF for court" generator |
| Audit search engine | ◐ | `GET /v1/audit/events` supports filters; no full-text |

**Priority gaps**

1. Read auditing for sensitive surfaces (vehicle lookup by plate,
   citizen lookup by national_id). One `audit_events` write per
   read; toggleable per-route at the gateway.
2. Forensic timeline UI in `web-compliance` — input `actor_user`,
   output paginated chronological feed across all services.
3. Exportable legal report generator — PDF with `audit_verify` proof
   appended, signed by ministry key.

---

## 9. Regulation & Policy Engine

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Offense definitions | ✓ | `regulation_offences` + country pack manifest |
| Law versioning | ✓ | `country_packs.version` + `superseded_by` |
| Municipality overrides | ✗ | See §5 |
| Regional regulation layers | ✗ | Same |
| Emergency regulations | ✗ | No time-bounded override shape |
| Effective dates | ✓ | `country_packs.effective_from` |
| Escalation policies | ✓ | `regulation_escalation` |
| Policy simulation readiness | ✗ | No "if I changed offense X to Y, how many fines would have been issued differently?" tool |
| Regulation history | ◐ | `country_packs` rows are immutable; no human-readable diff between versions |

**Priority gaps**

1. Time-bounded overrides — `regulation_overrides(tenant_id,
   region_code, valid_from, valid_until, override_jsonb)`.
2. Country-pack diff UI in `web-admin` for v1.0 → v1.1 review.
3. Eventual carve-out of `naditos-regulations` service (see
   `docs/ROADMAP.md` §5).

---

## 10. Citizen Self-Service Portal

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Profile management | ◐ | `/v1/citizens/me/owner` exists; no edit-my-profile flow |
| License renewal | ✗ | See §3 |
| Fine payment | ✓ | Webhook + idempotent paid transition |
| Inspection booking | ✗ | See §6 |
| Ownership transfer requests | ✓ | Citizen-to-citizen flow with code (0009) |
| Officer verification | ✗ | No `GET /v1/officers/by-badge/{n}` public endpoint |
| Corruption reporting | ✗ | No anonymous channel |
| Document uploads | ✗ | See §13 |
| Notifications | ✓ | Citizen inbox via `notifications` consumer (0005) |
| Digital certificates | ✗ | No wallet |

**Priority gaps**

1. Officer-verification public endpoint (rate-limited; returns badge
   + agency + photo URL; no PII).
2. Corruption reporting endpoint — append-only `incident_reports`
   table, hash-chained, anonymous by design.
3. Citizen profile edit endpoint with audit emission.

---

## 11. Ministry Command & Intelligence Center

| Sub-capability | State | Evidence / gap |
|---|---|---|
| National KPIs | ◐ | Home dashboard pulls vehicles/fines/alerts counts; no time-series |
| Regional analytics | ✗ | Region not modeled |
| Enforcement heatmaps | ✗ | No geospatial aggregation; fines have lat/lng but no map UI |
| Corruption indicators | ◐ | `audit_alerts` rolled up; no "top 10 officers by cancellation rate" surface |
| Operational metrics | ◐ | `/metrics` per service (Prom-shaped); no central dashboard |
| Inspection analytics | ✗ | No "pass rate by station" report |
| ANPR monitoring | ✗ | No live ANPR feed in admin |
| Executive reporting | ✗ | No printable monthly digest |
| Policy impact tracking | ✗ | "Did the new mobile-phone fine reduce repeat offenses?" — unanswerable |
| National intelligence | ✗ | Not modeled |

**Priority gaps**

1. Regions / municipalities table. Unblocks 5 of the 10 lines above.
2. Time-series rollup tables (`daily_kpis` per tenant + region) +
   nightly job. Powers the heatmap and trends.
3. Live ANPR websocket feed → `web-admin` `/anpr` page.

---

## 12. Notification & Communication System

| Sub-capability | State | Evidence / gap |
|---|---|---|
| SMS | ✗ | `Sender` interface in `notifications`; no Twilio / Vonage adapter |
| Email | ✗ | Same — interface only |
| Push notifications | ✗ | No web-push integration |
| Official notices | ◐ | Citizen inbox renders 7 templates; no print/PDF |
| Escalation alerts | ◐ | Fines escalation emits events; not yet wired to email/SMS provider |
| Multilingual support | ◐ | `i18n.ts` exists; renderers are English-only |
| Reminder workflows | ✗ | No "your inspection expires in 7 days" cron |
| Broadcast notices | ✗ | No "all citizens in region X" sender |

**Priority gaps**

1. Wire **one** real provider (Twilio sandbox) for SMS; emit on fine
   issuance. Validates the Sender contract end-to-end.
2. Reminder cron — daily job, scans vehicles whose `inspection_expires_at`
   is in [now, now + 14d], emits a notification.
3. Localised renderers per country pack `locales`.

---

## 13. Document & Digital Evidence System

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Secure uploads | ✗ | No uploader; `s3_key` is client-claimed |
| Digital signatures | ◐ | `country_packs.signature` BYTEA column; no signer / verifier |
| OCR readiness | ✗ | No OCR integration point |
| Metadata integrity | ✓ | `fine_evidence.sha256 / bytes` columns |
| Evidence chain tracking | ✓ | `audit_events` + `evidence_sealed` |
| Certificate generation | ✗ | No PDF / signed-image generator |
| Verification workflows | ✗ | "Is this certificate I'm holding real?" — no endpoint |

**Priority gaps**

1. S3-compatible uploader service (`services/storage` or build into
   gateway). Server signs presigned upload URLs; computes sha256 on
   completion; writes the row.
2. Certificate generator — PDF with vehicle + ministry seal + QR for
   verification.
3. Public `GET /v1/certificates/{id}/verify` — returns the signed
   payload + verification result.

---

## 14. ANPR & Smart Enforcement

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Plate recognition workflows | ✓ | `anpr-gateway` service with normalization |
| Hotlist detection | ✓ | `audit_alerts` on scan-matching-wanted vehicle |
| Stolen vehicle alerts | ✓ | Same |
| Insurance expiry alerts | ✗ | No worker that emits an alert on expired-insurance scan |
| Inspection expiry alerts | ✗ | Same |
| Geolocation tracking | ✓ | `anpr_scans.geo_lat / geo_lng` |
| Camera ingestion readiness | ◐ | Webhook receiver shape; no production ingest pipeline |
| Evidence retention | ✓ | `evidence_retention_policy` per tenant (0004) |

**Priority gaps**

1. Insurance + inspection expiry alerts on ANPR scans (lift the
   stolen-vehicle pattern; trivial).
2. Per-camera identity + healthcheck (`anpr_cameras` table).
3. Bulk ingest endpoint for fixed cameras pushing 100s of scans/sec.

---

## 15. National Data Governance

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Jurisdiction hierarchy | ◐ | One level: tenant. No country → region → municipality nesting |
| Municipality hierarchy | ✗ | Not modeled |
| Agency hierarchy | ✗ | `officer_profiles.agency` text only |
| Tenant isolation | ✓ | RLS |
| Archival strategy | ✗ | No `pg_dump` cron; no cold-storage shape |
| Backup / recovery | ✗ | Fly auto-backups on PG; never tested restore |
| Data retention policies | ✓ | `evidence_retention_policy` per tenant |
| Legal retention compliance | ◐ | Retention is configured but not enforced by a reaper for every entity |

**Priority gaps**

1. Disaster recovery dry run: dump → restore → smoke. Time it.
2. Regions + municipalities + agencies tables (one PR, unblocks
   §11 and §5).
3. Retention reaper covering every PII-bearing table, not just
   evidence.

---

## 16. Observability & Operations

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Centralized logs | ◐ | Structured slog to stdout; no Loki yet (Phase 2 in ROADMAP) |
| Metrics | ◐ | `/metrics` per service (Prom-shaped); no Prometheus scraper deployed |
| Distributed tracing | ◐ | request-id + trace-id in logs; no OTLP collector |
| Uptime monitoring | ✗ | No external pinger |
| Deployment tracking | ✓ | git SHA in boot logs (PR #11) |
| Rollback readiness | ◐ | `fly machine restart --image <sha>` works; no documented runbook |
| Health dashboards | ✗ | No Grafana |
| Alerting | ✗ | No on-call rota or paging |

**Priority gaps** (all are Phase 2 in `docs/ROADMAP.md`)

1. Deploy Loki + Prometheus + Grafana as four extra Fly apps.
2. OTel collector + Tempo for tracing.
3. Define + publish SLOs.

---

## 17. Security Hardening

| Sub-capability | State | Evidence / gap |
|---|---|---|
| Rate limiting | ✓ | Per-tenant per-prefix token bucket (`services/gateway/internal/proxy/ratelimit.go`) |
| WAF readiness | ✗ | No WAF; Fly proxy is HTTPS-only terminating |
| Secret rotation readiness | ✓ | Runbooks in `deploy/fly/README.md` for JWT_SECRET, ADMIN_BOOTSTRAP_KEY, DATABASE_URL |
| Encrypted backups | ◐ | Fly Postgres volumes encrypted at rest; off-site backup not configured |
| Audit immutability | ✓ | DB triggers raising exceptions |
| Intrusion monitoring readiness | ✗ | No IDS; no SIEM integration |
| Least-privilege RBAC | ◐ | Role granularity is coarse (admin / officer / citizen / court); no per-action permission tuning UI |
| Tenant isolation enforcement | ✓ | RLS + per-service tenant claim verification at gateway |
| API throttling | ✓ | See rate limiting |
| Evidence integrity protection | ◐ | Server-side sha256 not yet computed (see §13) |

**Priority gaps**

1. **Server-side sha256 on evidence upload** — biggest concrete
   integrity hole today. Blocks "evidence chain" claims to a court.
2. Per-route audit of "least-privileged role required" — write up
   the matrix.
3. Off-site encrypted backup of `naditos-pg` to S3-compatible
   storage.

---

## 18. DevOps & Platform Governance

| Sub-capability | State | Evidence / gap |
|---|---|---|
| CI/CD | ◐ | `.github/workflows/ci.yml` runs go build/vet, integration tests, smoke, govulncheck, pnpm build. Smoke currently red (chronic OOM, fix in flight) |
| Protected branches | ✓ | Branch protection on `main` requires 1 review + linear history |
| Staging / production separation | ✗ | Prod only |
| Deployment approvals | ◐ | Branch protection requires review for merge; no separate "approve deploy" step |
| Migration discipline | ◐ | Numbered `.up.sql` + `.down.sql`; no migration test in CI yet (drafted in roadmap) |
| Rollback pipelines | ✗ | Ad-hoc via `fly machine restart --image <sha>` |
| Infrastructure-as-code readiness | ◐ | `fly.*.toml` per service; K8s manifests scaffolded only |
| Environment consistency | ◐ | One env (prod); local dev via docker-compose; staging missing |
| Deployment observability | ◐ | git SHA in boot logs; no central deploy dashboard |

**Priority gaps**

1. **Get CI smoke green.** The chronic-OOM fix is staged in PR #11;
   when it lands, branch-protection gates start meaningfully blocking
   bad merges.
2. Staging Fly org. Mirror of prod, target of "merge to `main`"
   auto-deploy.
3. Migration test in CI — apply every `.up.sql`, then reverse every
   `.down.sql`, assert schema is clean.

---

## Cross-cutting inconsistencies (the "drift" the platform should not have)

These are the things that, left unaddressed, cause the kind of
fragmentation the prompt warns against.

| Drift | Where it shows | Smallest correction |
|---|---|---|
| Region not modeled | §5, §11, §15 all blocked on it | One PR: `regions`, `municipalities`, `agencies` tables + FKs |
| Evidence is client-claimed | §2, §4, §13, §17 all weaken on this | One PR: server-side uploader + sha256 |
| Read auditing missing | §8 weakens; ministry can't see who looked at what | One PR: gateway middleware that audits configured read routes |
| Notification adapter never plugged in | §12 entirely; §5 escalation can't actually notify | One PR: wire Twilio sandbox + email SMTP fallback |
| No staging environment | §18; every change tests on prod | One PR: second Fly org + CI auto-deploy target |
| Audit data sources fragmented across `audit_events`, `audit_alerts`, `officer_daily_stats` | §8, §11 each render differently | One PR: unify the read API behind `/v1/audit/*` semantics |

---

## Proposed sequence (the next ~10 PRs in priority order)

This is *not* a feature roadmap — it's a stabilization sequence. Each
PR closes a foundational gap above, none introduces new product surface.

1. **CI smoke green** (in flight on PR #11). Until this lands, no
   downstream PR can be trusted.
2. **Read auditing middleware** at the gateway, configured per-route
   in `routes.go`. Closes §8 access-auditing gap.
3. **Regions / municipalities / agencies** schema + FK migrations.
   Unblocks §5, §11, §15.
4. **Server-side evidence uploader** (`services/storage` or built
   into gateway). Closes §2 + §4 + §13 + §17.
5. **Login event audit** — `auth` service emits `login.success` /
   `login.failed` to `audit_events`. Closes §1 read auditing.
6. **Twilio sandbox + SMTP fallback** wired into `notifications`
   consumer. Closes §5 + §12.
7. **Inspection scheduling** (`inspection_appointments` + queue tile
   in `web-inspection`). Closes §6.
8. **License renewal endpoint** + citizen UI. Closes §3.
9. **Migration test in CI** — apply-all then reverse-all. Closes a
   §18 gap.
10. **Staging Fly org + auto-deploy on merge.** Closes §18.

Each is one PR. Each PR description should reference back to this
document's section it closes.

---

## What this document deliberately does NOT do

- Define UI mockups. Capability ≠ pixel-perfect screen.
- Promise dates. Sequencing yes; calendar no.
- Pre-commit to which sub-capability becomes a separate service.
  Extraction follows the rule "three or more consumers" (see
  ROADMAP §5).
- Cover AI integration. Phase 5 in ROADMAP; not Phase 1.
- Cover sovereign-cloud migration. Phase 6 in ROADMAP.

---

## Appendix — how this audit was produced

For every capability:

1. Read the relevant migration(s), service code, and frontend pages.
2. Marked ✓ only if the code path is exercised in tests, smoke, or
   the live deployment.
3. Marked ◐ only if a schema column / endpoint exists but is not
   driven by any caller, OR if a flow exists for one persona but not
   the parallel ones.
4. Marked ✗ if no schema, no endpoint, no UI exists.

If you disagree with a row, the disagreement itself is the actionable
thing — open a PR to either (a) correct the row, or (b) commit the
missing code that makes the row true.
