-- audit_alerts: append-only signal pipeline for the audit dashboard.
--
-- The rollup job emits one row per detected anomaly per (tenant,
-- subject, day) into this table. Admins triage by setting resolved_at
-- via POST /v1/audit/alerts/{id}/resolve; the unique partial index
-- below makes the rollup's UPSERT idempotent so a single anomaly
-- doesn't get duplicated across sweeps.
--
-- Detection lives in services/audit/internal/rollup; this table is
-- the persistence layer those detectors write to.

CREATE TABLE audit_alerts (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,
  subject_kind  TEXT,
  subject_id    UUID,
  day           DATE NOT NULL,
  severity      REAL,
  details       JSONB NOT NULL,
  detected_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at   TIMESTAMPTZ,
  resolved_by   UUID,
  resolution    TEXT
);

CREATE INDEX audit_alerts_open_idx
  ON audit_alerts(tenant_id, detected_at DESC)
  WHERE resolved_at IS NULL;

-- Idempotency: at most one OPEN alert per (tenant, kind, subject, day).
-- Once resolved, a fresh detection on the same day legitimately re-fires.
CREATE UNIQUE INDEX audit_alerts_uniq_open_idx
  ON audit_alerts(tenant_id, kind, subject_id, day)
  WHERE resolved_at IS NULL;

ALTER TABLE audit_alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_alerts
  USING (tenant_id = app_tenant())
  WITH CHECK (tenant_id = app_tenant());
