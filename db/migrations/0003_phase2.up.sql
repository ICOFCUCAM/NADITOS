-- ============================================================================
-- NADITOS Phase-2 schema additions:
--   Driver license lifecycle (endorsements, violations, suspensions, demerit)
--   ANPR async pipeline (queue + reconciliation)
--   Provider health + retry queue
--   Evidence chain-of-custody + retention policies
--   Country regulation packs
--   Officer activity rollups (for anti-corruption analytics)
--   Outbox for reliable event publishing
-- ============================================================================

-- ─── Driver license: endorsements, violations, suspensions ─────────────────

CREATE TABLE driver_endorsements (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  license_id      UUID NOT NULL REFERENCES driver_licenses(id) ON DELETE CASCADE,
  code            TEXT NOT NULL,                     -- e.g. 'CORRECTIVE_LENSES'
  description     TEXT,
  issued_at       DATE NOT NULL,
  expires_at      DATE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_endorsements_license_idx ON driver_endorsements(license_id);

CREATE TABLE driver_violations (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  license_id      UUID NOT NULL REFERENCES driver_licenses(id) ON DELETE CASCADE,
  fine_id         UUID REFERENCES fines(id) ON DELETE SET NULL,
  offence_code    TEXT NOT NULL,
  points          INT NOT NULL DEFAULT 0,
  occurred_at     TIMESTAMPTZ NOT NULL,
  recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_violations_license_idx ON driver_violations(license_id);
CREATE INDEX driver_violations_occurred_idx ON driver_violations(license_id, occurred_at DESC);

CREATE TABLE driver_suspensions (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  license_id      UUID NOT NULL REFERENCES driver_licenses(id) ON DELETE CASCADE,
  reason          TEXT NOT NULL,
  trigger_kind    TEXT NOT NULL CHECK (trigger_kind IN ('demerit','court','medical','administrative')),
  starts_at       TIMESTAMPTZ NOT NULL,
  ends_at         TIMESTAMPTZ,
  lifted_at       TIMESTAMPTZ,
  created_by      UUID REFERENCES users(id),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_suspensions_license_idx ON driver_suspensions(license_id);

-- Append-only ledger of demerit point movements; the license.points column
-- is a denormalized cache, recomputed by the suspension engine.
CREATE TABLE driver_demerit_events (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       TEXT NOT NULL,
  license_id      UUID NOT NULL REFERENCES driver_licenses(id) ON DELETE CASCADE,
  delta           INT NOT NULL,                       -- +N for violations, -N for clearance
  reason          TEXT NOT NULL,
  source          TEXT NOT NULL,                     -- 'fine','court','expiry','manual'
  source_id       TEXT,
  occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX driver_demerit_license_idx ON driver_demerit_events(license_id, occurred_at DESC);

-- Per-tenant demerit policy: at how many points does suspension trigger,
-- for how long, and what's the rolling window?
CREATE TABLE driver_demerit_policy (
  tenant_id           TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
  threshold_points    INT NOT NULL DEFAULT 12,
  window_months       INT NOT NULL DEFAULT 24,
  suspension_months   INT NOT NULL DEFAULT 6,
  reset_after_months  INT NOT NULL DEFAULT 36
);

-- Driver license biometric refs (template stored in HSM, never in this DB).
-- This is just a pointer + metadata so verification can attest "the person
-- in front of me matches the template registered at issuance".
CREATE TABLE driver_biometrics (
  license_id      UUID PRIMARY KEY REFERENCES driver_licenses(id) ON DELETE CASCADE,
  tenant_id       TEXT NOT NULL,
  template_kid    TEXT NOT NULL,                     -- key id in HSM/KMS
  template_hash   BYTEA NOT NULL,                    -- SHA-256(template) for tamper detect
  algo            TEXT NOT NULL,                     -- e.g. 'iso19794-fingerprint', 'face-embed-v1'
  enrolled_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── ANPR async pipeline ──────────────────────────────────────────────────
-- The gateway accepts a scan event and queues it; a worker normalizes the
-- plate, checks duplicates, matches against vehicles, and publishes the
-- domain event. The queue lets us survive provider outages.

CREATE TYPE anpr_job_status AS ENUM ('queued','processing','done','failed','duplicate');

CREATE TABLE anpr_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  scan_id         UUID REFERENCES anpr_scans(id) ON DELETE SET NULL,
  source          TEXT NOT NULL,
  source_id       TEXT,
  raw_plate       TEXT NOT NULL,
  normalized_plate TEXT,
  confidence      REAL NOT NULL,
  geo_lat         DOUBLE PRECISION,
  geo_lng         DOUBLE PRECISION,
  image_s3_key    TEXT,
  captured_at     TIMESTAMPTZ NOT NULL,
  status          anpr_job_status NOT NULL DEFAULT 'queued',
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT,
  enqueued_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at    TIMESTAMPTZ
);
CREATE INDEX anpr_jobs_status_idx ON anpr_jobs(status, enqueued_at)
  WHERE status IN ('queued','processing');
CREATE INDEX anpr_jobs_dedup_idx ON anpr_jobs(tenant_id, normalized_plate, captured_at);

-- ─── Provider health + retry ──────────────────────────────────────────────
-- Generic across modules: payments, anpr, insurance, inspection, court,
-- notifications, identity. Used by health endpoints and dashboards.

CREATE TABLE provider_health (
  tenant_id       TEXT NOT NULL,
  module          TEXT NOT NULL,                     -- 'insurance', 'anpr', ...
  provider        TEXT NOT NULL,
  region          TEXT,
  last_ok_at      TIMESTAMPTZ,
  last_fail_at    TIMESTAMPTZ,
  fail_streak     INT NOT NULL DEFAULT 0,
  state           TEXT NOT NULL DEFAULT 'unknown',   -- ok|degraded|down|unknown
  details         JSONB,
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, module, provider)
);

-- Generic retry queue. Workers in each service poll for due jobs.
CREATE TYPE retry_job_status AS ENUM ('queued','running','done','failed','dead_letter');

CREATE TABLE retry_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  module          TEXT NOT NULL,
  kind            TEXT NOT NULL,                     -- 'insurance.verify', 'court.file', etc.
  payload         JSONB NOT NULL,
  status          retry_job_status NOT NULL DEFAULT 'queued',
  attempts        INT NOT NULL DEFAULT 0,
  max_attempts    INT NOT NULL DEFAULT 5,
  next_run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX retry_jobs_due_idx ON retry_jobs(status, next_run_at)
  WHERE status IN ('queued','running');

-- ─── Evidence chain-of-custody ────────────────────────────────────────────
-- Every action on an evidence object is appended here; tamper-detection is
-- via the audit hash chain plus the per-object SHA-256 already on
-- fine_evidence. Retention policy can be tenant-overridden.

CREATE TABLE evidence_custody (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       TEXT NOT NULL,
  fine_id         UUID NOT NULL REFERENCES fines(id) ON DELETE CASCADE,
  evidence_id     UUID NOT NULL REFERENCES fine_evidence(id) ON DELETE CASCADE,
  action          TEXT NOT NULL,                     -- captured|uploaded|verified|viewed|exported|sealed
  actor_user      UUID,
  actor_role      TEXT,
  actor_device    TEXT,
  details         JSONB,
  occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX evidence_custody_evidence_idx ON evidence_custody(evidence_id, occurred_at);

CREATE TABLE evidence_retention_policy (
  tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
  default_days    INT NOT NULL DEFAULT 1825,         -- 5y default
  paid_fine_days  INT NOT NULL DEFAULT 1825,
  cancelled_fine_days INT NOT NULL DEFAULT 365,
  court_case_days INT,                                -- NULL = retain forever
  legal_hold_active BOOLEAN NOT NULL DEFAULT false
);

-- ─── Country regulation packs ─────────────────────────────────────────────
-- A "pack" is a versioned bundle of regulation_offences, escalation,
-- locale strings, and policy parameters that defines how the platform
-- enforces transport law in a given country/jurisdiction. Tenants can be
-- pinned to a specific pack version; new packs are published, reviewed,
-- and applied via apply_country_pack().

CREATE TABLE country_packs (
  id              TEXT PRIMARY KEY,                  -- e.g. 'NO-2026-01'
  country_code    TEXT NOT NULL,
  version         TEXT NOT NULL,
  effective_from  DATE NOT NULL,
  superseded_by   TEXT REFERENCES country_packs(id),
  manifest        JSONB NOT NULL,                    -- offences, locales, plate regex, currency, etc.
  signature       BYTEA,                             -- detached signature by ministry's key
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_country_pack (
  tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
  pack_id         TEXT NOT NULL REFERENCES country_packs(id),
  applied_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Officer activity (anti-corruption analytics) ─────────────────────────
-- Pre-aggregated rollups so the dashboard can flag outliers cheaply.
-- Computed nightly by a job; snapshot per (officer, day, tenant).

CREATE TABLE officer_daily_stats (
  tenant_id       TEXT NOT NULL,
  officer_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day             DATE NOT NULL,
  fines_issued    INT NOT NULL DEFAULT 0,
  fines_cancelled INT NOT NULL DEFAULT 0,
  fines_total     NUMERIC(14,2) NOT NULL DEFAULT 0,
  scans_run       INT NOT NULL DEFAULT 0,
  unique_plates   INT NOT NULL DEFAULT 0,
  -- Anomaly score 0..1; computed by the anomaly job. NULL = not computed yet.
  anomaly_score   REAL,
  PRIMARY KEY (tenant_id, officer_id, day)
);

-- ─── Reliable event outbox ───────────────────────────────────────────────
-- Producers insert into outbox in the same DB transaction as their state
-- change; a relay process publishes them to NATS/Kafka and marks delivered.
-- Guarantees no event is lost even if the bus is down at insert time.

CREATE TABLE event_outbox (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       TEXT NOT NULL,
  envelope        JSONB NOT NULL,                    -- full Envelope
  delivered_at    TIMESTAMPTZ,
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX event_outbox_pending_idx ON event_outbox(created_at)
  WHERE delivered_at IS NULL;

-- ─── RLS for the new tables ───────────────────────────────────────────────
DO $$
DECLARE t TEXT;
BEGIN
  FOR t IN SELECT unnest(ARRAY[
    'driver_endorsements','driver_violations','driver_suspensions',
    'driver_demerit_events','driver_demerit_policy','driver_biometrics',
    'anpr_jobs','provider_health','retry_jobs',
    'evidence_custody','evidence_retention_policy',
    'tenant_country_pack','officer_daily_stats','event_outbox'
  ]) LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I
         USING (tenant_id = app_tenant())
         WITH CHECK (tenant_id = app_tenant())', t);
  END LOOP;
END $$;

-- ─── View: live driver standing ──────────────────────────────────────────
CREATE VIEW v_driver_standing AS
SELECT
  l.id            AS license_id,
  l.tenant_id,
  l.license_number,
  l.full_name,
  l.points,
  l.is_suspended,
  l.expires_at,
  CASE
    WHEN l.is_suspended OR (l.suspended_until IS NOT NULL AND l.suspended_until > CURRENT_DATE)
                                                          THEN 'suspended'
    WHEN l.expires_at IS NULL OR l.expires_at < CURRENT_DATE THEN 'expired'
    WHEN l.points >= 12                                   THEN 'at_risk'
    WHEN l.expires_at < CURRENT_DATE + INTERVAL '60 days' THEN 'expiring_soon'
    ELSE 'good'
  END AS standing,
  (SELECT count(*) FROM driver_violations v
     WHERE v.license_id = l.id
       AND v.occurred_at > now() - interval '24 months') AS recent_violations
FROM driver_licenses l;
