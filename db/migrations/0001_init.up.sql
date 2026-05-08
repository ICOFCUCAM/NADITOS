-- ============================================================================
-- NADITOS — Phase 1 schema
-- Multi-tenant via Postgres RLS. Every domain row carries tenant_id.
-- Services set: SET LOCAL app.tenant_id, app.user_id, app.role
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";

-- ─── Helper: current tenant from session var ───────────────────────────────
CREATE OR REPLACE FUNCTION app_tenant() RETURNS TEXT
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')
$$;

CREATE OR REPLACE FUNCTION app_role() RETURNS TEXT
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.role', true), '')
$$;

CREATE OR REPLACE FUNCTION app_user() RETURNS UUID
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.user_id', true), '')::UUID
$$;

-- ============================================================================
-- TENANTS — countries / agencies
-- ============================================================================
CREATE TABLE tenants (
  id              TEXT PRIMARY KEY,                    -- e.g. 'NO', 'DE-BAY'
  name            TEXT NOT NULL,
  country_code    TEXT NOT NULL,                       -- ISO-3166-1 alpha-2
  default_locale  TEXT NOT NULL DEFAULT 'en',
  currency        TEXT NOT NULL DEFAULT 'EUR',
  plate_regex     TEXT NOT NULL DEFAULT '^[A-Z0-9-]{2,10}$',
  modules         JSONB NOT NULL DEFAULT '{}',         -- which modules enabled
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- USERS / ROLES / RBAC
-- ============================================================================
CREATE TABLE users (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email           CITEXT NOT NULL,
  phone           TEXT,
  password_hash   TEXT NOT NULL,                       -- bcrypt
  full_name       TEXT NOT NULL,
  national_id     TEXT,                                -- pii
  is_active       BOOLEAN NOT NULL DEFAULT true,
  mfa_secret      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, email)
);
CREATE INDEX users_tenant_idx ON users(tenant_id);

CREATE TABLE roles (
  tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  code        TEXT NOT NULL,                           -- 'officer','admin','citizen','court','customs'
  name        TEXT NOT NULL,
  PRIMARY KEY (tenant_id, code)
);

CREATE TABLE role_permissions (
  tenant_id   TEXT NOT NULL,
  role_code   TEXT NOT NULL,
  permission  TEXT NOT NULL,                           -- 'fines:create', 'registry:read', etc.
  PRIMARY KEY (tenant_id, role_code, permission),
  FOREIGN KEY (tenant_id, role_code) REFERENCES roles(tenant_id, code) ON DELETE CASCADE
);

CREATE TABLE user_roles (
  tenant_id   TEXT NOT NULL,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_code   TEXT NOT NULL,
  PRIMARY KEY (tenant_id, user_id, role_code),
  FOREIGN KEY (tenant_id, role_code) REFERENCES roles(tenant_id, code) ON DELETE CASCADE
);

-- Officer profiles add agency / jurisdiction / device-binding
CREATE TABLE officer_profiles (
  user_id         UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  badge_number    TEXT NOT NULL,
  agency          TEXT NOT NULL,
  jurisdiction    TEXT,
  device_id       TEXT,                                -- bound device
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, badge_number)
);

-- Refresh tokens (hashed)
CREATE TABLE refresh_tokens (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash    TEXT NOT NULL,
  device_id     TEXT,
  expires_at    TIMESTAMPTZ NOT NULL,
  revoked_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens(user_id) WHERE revoked_at IS NULL;

-- ============================================================================
-- REGULATION ENGINE — per tenant
-- ============================================================================
CREATE TABLE regulation_offences (
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  code          TEXT NOT NULL,                         -- 'INS_EXPIRED','INSP_EXPIRED','SPEED_30'
  name          JSONB NOT NULL,                        -- {"en":"Expired insurance","fr":"..."}
  base_amount   NUMERIC(12,2) NOT NULL,
  currency      TEXT NOT NULL,
  rule_expr     TEXT,                                  -- machine-checkable hint
  duplicate_window_min INT NOT NULL DEFAULT 1440,
  is_active     BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (tenant_id, code)
);

CREATE TABLE regulation_escalation (
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  stage         INT NOT NULL CHECK (stage BETWEEN 1 AND 5),
  after_days    INT NOT NULL,
  multiplier    NUMERIC(4,2) NOT NULL DEFAULT 1.0,
  action        TEXT NOT NULL,                         -- warning|penalty|flag|seize|court
  PRIMARY KEY (tenant_id, stage)
);

-- ============================================================================
-- VEHICLE REGISTRY
-- ============================================================================
CREATE TABLE owners (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id       UUID REFERENCES users(id),             -- if a citizen account
  full_name     TEXT NOT NULL,
  national_id   TEXT,                                  -- pii
  email         CITEXT,
  phone         TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX owners_tenant_idx ON owners(tenant_id);

CREATE TABLE vehicles (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  plate           TEXT NOT NULL,
  vin             TEXT,
  chassis_number  TEXT,
  make            TEXT,
  model           TEXT,
  year            INT,
  color           TEXT,
  category        TEXT,                                -- car|motorcycle|truck|bus|...
  emission_class  TEXT,                                -- EURO_6 etc.
  owner_id        UUID REFERENCES owners(id),
  registration_expires_at  TIMESTAMPTZ,
  insurance_expires_at     TIMESTAMPTZ,
  inspection_expires_at    TIMESTAMPTZ,
  tax_paid_through         TIMESTAMPTZ,
  is_stolen       BOOLEAN NOT NULL DEFAULT false,
  is_seized       BOOLEAN NOT NULL DEFAULT false,
  is_wanted       BOOLEAN NOT NULL DEFAULT false,
  notes           TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, plate)
);
CREATE INDEX vehicles_tenant_idx  ON vehicles(tenant_id);
CREATE INDEX vehicles_plate_idx   ON vehicles(tenant_id, plate);
CREATE INDEX vehicles_vin_idx     ON vehicles(tenant_id, vin) WHERE vin IS NOT NULL;
CREATE INDEX vehicles_owner_idx   ON vehicles(owner_id);

-- ============================================================================
-- DRIVER LICENSES
-- ============================================================================
CREATE TABLE driver_licenses (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id         UUID REFERENCES users(id),
  license_number  TEXT NOT NULL,
  full_name       TEXT NOT NULL,
  date_of_birth   DATE,
  classes         TEXT[] NOT NULL DEFAULT '{}',        -- {'A','B','C','CE'}
  issued_at       DATE,
  expires_at      DATE,
  points          INT NOT NULL DEFAULT 0,
  is_suspended    BOOLEAN NOT NULL DEFAULT false,
  suspended_until DATE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, license_number)
);
CREATE INDEX driver_licenses_tenant_idx ON driver_licenses(tenant_id);

-- ============================================================================
-- INSURANCE / INSPECTION (verification records pulled from providers)
-- ============================================================================
CREATE TABLE insurance_records (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  vehicle_id    UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  provider      TEXT NOT NULL,
  policy_number TEXT NOT NULL,
  starts_at     TIMESTAMPTZ NOT NULL,
  expires_at    TIMESTAMPTZ NOT NULL,
  is_active     BOOLEAN NOT NULL DEFAULT true,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX insurance_vehicle_idx ON insurance_records(vehicle_id);

CREATE TABLE inspection_records (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  vehicle_id    UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  station       TEXT NOT NULL,
  performed_at  TIMESTAMPTZ NOT NULL,
  expires_at    TIMESTAMPTZ NOT NULL,
  result        TEXT NOT NULL CHECK (result IN ('pass','fail','conditional')),
  certificate_url TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX inspection_vehicle_idx ON inspection_records(vehicle_id);

-- ============================================================================
-- FINES
-- ============================================================================
CREATE TYPE fine_status AS ENUM (
  'issued','warned','overdue','paid','disputed','escalated','seized','court','cancelled'
);

CREATE TABLE fines (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  vehicle_id        UUID REFERENCES vehicles(id),
  plate             TEXT NOT NULL,                     -- denormalized for offline replay
  driver_user_id    UUID REFERENCES users(id),
  driver_license_id UUID REFERENCES driver_licenses(id),
  offence_code      TEXT NOT NULL,
  amount            NUMERIC(12,2) NOT NULL,
  currency          TEXT NOT NULL,
  status            fine_status NOT NULL DEFAULT 'issued',
  issued_by         UUID NOT NULL REFERENCES users(id),
  issued_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  device_id         TEXT,
  geo_lat           DOUBLE PRECISION,
  geo_lng           DOUBLE PRECISION,
  geo_accuracy_m    REAL,
  due_at            TIMESTAMPTZ NOT NULL,
  paid_at           TIMESTAMPTZ,
  escalation_stage  INT NOT NULL DEFAULT 0,
  notes             TEXT,
  FOREIGN KEY (tenant_id, offence_code)
    REFERENCES regulation_offences(tenant_id, code)
);
CREATE INDEX fines_tenant_idx     ON fines(tenant_id);
CREATE INDEX fines_vehicle_idx    ON fines(vehicle_id);
CREATE INDEX fines_status_idx     ON fines(tenant_id, status);
CREATE INDEX fines_issued_at_idx  ON fines(tenant_id, issued_at DESC);
CREATE INDEX fines_dup_idx        ON fines(tenant_id, vehicle_id, offence_code, issued_at);

CREATE TABLE fine_evidence (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  fine_id       UUID NOT NULL REFERENCES fines(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL CHECK (kind IN ('photo','video','signature','document')),
  s3_key        TEXT NOT NULL,
  sha256        TEXT NOT NULL,
  bytes         BIGINT NOT NULL,
  taken_at      TIMESTAMPTZ NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fine_evidence_fine_idx ON fine_evidence(fine_id);

CREATE TABLE fine_disputes (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  fine_id       UUID NOT NULL REFERENCES fines(id) ON DELETE CASCADE,
  filed_by      UUID NOT NULL REFERENCES users(id),
  reason        TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','accepted','rejected','court')),
  resolution    TEXT,
  filed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at   TIMESTAMPTZ
);

CREATE TABLE fine_payments (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  fine_id       UUID NOT NULL REFERENCES fines(id) ON DELETE CASCADE,
  amount        NUMERIC(12,2) NOT NULL,
  currency      TEXT NOT NULL,
  method        TEXT NOT NULL,                         -- 'card','mobile','treasury'
  provider_ref  TEXT,
  status        TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','succeeded','failed','refunded')),
  paid_at       TIMESTAMPTZ
);

-- Helper trigger: copy tenant_id from parent fine on insert if not provided.
CREATE OR REPLACE FUNCTION fine_child_tenant() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.tenant_id IS NULL THEN
    SELECT tenant_id INTO NEW.tenant_id FROM fines WHERE id = NEW.fine_id;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER fine_evidence_set_tenant BEFORE INSERT ON fine_evidence
  FOR EACH ROW EXECUTE FUNCTION fine_child_tenant();
CREATE TRIGGER fine_disputes_set_tenant BEFORE INSERT ON fine_disputes
  FOR EACH ROW EXECUTE FUNCTION fine_child_tenant();
CREATE TRIGGER fine_payments_set_tenant BEFORE INSERT ON fine_payments
  FOR EACH ROW EXECUTE FUNCTION fine_child_tenant();

-- ============================================================================
-- ANPR scans (raw events from cameras / officer captures)
-- ============================================================================
CREATE TABLE anpr_scans (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id),
  plate_read    TEXT NOT NULL,
  confidence    REAL NOT NULL,
  source        TEXT NOT NULL,                         -- 'officer','fixed_cam','toll','border'
  source_id     TEXT,
  captured_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  geo_lat       DOUBLE PRECISION,
  geo_lng       DOUBLE PRECISION,
  image_s3_key  TEXT,
  matched_vehicle_id UUID REFERENCES vehicles(id)
);
CREATE INDEX anpr_tenant_time_idx ON anpr_scans(tenant_id, captured_at DESC);
CREATE INDEX anpr_plate_idx       ON anpr_scans(tenant_id, plate_read);

-- ============================================================================
-- AUDIT — append-only, hash-chained
-- ============================================================================
CREATE TABLE audit_events (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     TEXT NOT NULL,
  occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  actor_user    UUID,
  actor_role    TEXT,
  actor_device  TEXT,
  actor_ip      INET,
  service       TEXT NOT NULL,
  action        TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id   TEXT,
  before        JSONB,
  after         JSONB,
  prev_hash     BYTEA,
  hash          BYTEA NOT NULL
);
CREATE INDEX audit_tenant_time_idx ON audit_events(tenant_id, occurred_at DESC);
CREATE INDEX audit_resource_idx    ON audit_events(resource_type, resource_id);
CREATE INDEX audit_actor_idx       ON audit_events(actor_user);

-- Block UPDATE/DELETE on audit_events at the database level.
CREATE OR REPLACE FUNCTION audit_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only';
END $$;
CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION audit_immutable();
CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION audit_immutable();

-- ============================================================================
-- VIEW: live vehicle status
-- ============================================================================
CREATE VIEW v_vehicle_status AS
SELECT
  v.id,
  v.tenant_id,
  v.plate,
  v.owner_id,
  CASE
    WHEN v.is_stolen OR v.is_seized OR v.is_wanted        THEN 'black'
    WHEN v.insurance_expires_at  IS NULL
      OR v.insurance_expires_at  < now()                  THEN 'red'
    WHEN v.inspection_expires_at IS NULL
      OR v.inspection_expires_at < now()                  THEN 'red'
    WHEN v.registration_expires_at IS NOT NULL
      AND v.registration_expires_at < now()               THEN 'red'
    WHEN v.insurance_expires_at  < now() + interval '30 days'
      OR v.inspection_expires_at < now() + interval '30 days'
      OR (v.tax_paid_through IS NOT NULL
          AND v.tax_paid_through < now() + interval '30 days')
                                                          THEN 'yellow'
    ELSE 'green'
  END AS status,
  v.insurance_expires_at,
  v.inspection_expires_at,
  v.registration_expires_at,
  v.tax_paid_through,
  v.is_stolen, v.is_seized, v.is_wanted
FROM vehicles v;

-- ============================================================================
-- ROW-LEVEL SECURITY
-- ============================================================================
DO $$
DECLARE t TEXT;
BEGIN
  FOR t IN SELECT unnest(ARRAY[
    'users','roles','role_permissions','user_roles','officer_profiles',
    'refresh_tokens','regulation_offences','regulation_escalation',
    'owners','vehicles','driver_licenses','insurance_records',
    'inspection_records','fines','fine_evidence','fine_disputes',
    'fine_payments','anpr_scans','audit_events'
  ]) LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I
         USING (tenant_id = app_tenant())
         WITH CHECK (tenant_id = app_tenant())', t);
  END LOOP;
END $$;

-- A super-role bypass for migration/admin scripts (DB role only, never JWT)
CREATE ROLE naditos_admin NOINHERIT;
GRANT ALL ON ALL TABLES IN SCHEMA public TO naditos_admin;
ALTER ROLE naditos_admin SET row_security = off;
