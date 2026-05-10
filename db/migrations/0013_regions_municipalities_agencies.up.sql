-- ============================================================================
-- 0013_regions_municipalities_agencies.up.sql
--
-- Geographic / organizational hierarchy. Until now every row carried only
-- tenant_id, so a national deployment could not distinguish a fine issued
-- in Region A from one issued in Region B, nor attribute an inspection to
-- a specific agency. That made cross-region analytics, jurisdiction-scoped
-- RBAC, and inter-agency cooperation logging impossible.
--
-- Capability audit §5 (geographic federation), §11 (jurisdictional
-- boundaries), §15 (multi-agency cooperation) are unblocked by this
-- migration. The columns added to existing domain tables are NULLable so
-- the migration is backward-compatible with rows seeded before today.
-- Subsequent enforcement (NOT NULL, RLS predicate extensions) lands once
-- service-side writers populate the new columns end-to-end.
-- ============================================================================

CREATE TABLE regions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  -- Human-stable code (e.g. ISO-3166-2 "NG-LA" for Lagos, "NO-03" for Oslo).
  -- Used in URLs and audit log resource ids, so it should be lowercase and
  -- never collide within a tenant.
  code        TEXT NOT NULL,
  name        TEXT NOT NULL,
  -- Free-form attributes the country pack wants to surface (population,
  -- capital municipality id once seeded, GIS bbox, etc.).
  metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);

CREATE TABLE municipalities (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  region_id   UUID NOT NULL REFERENCES regions(id) ON DELETE CASCADE,
  code        TEXT NOT NULL,
  name        TEXT NOT NULL,
  -- kind distinguishes city / town / county / district. Country packs
  -- map their native terms onto this small set so cross-country queries
  -- still compose.
  kind        TEXT NOT NULL DEFAULT 'municipality'
               CHECK (kind IN ('city','town','county','district','municipality','other')),
  metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);
CREATE INDEX municipalities_region_idx ON municipalities (tenant_id, region_id);

-- Agencies are the *operating* entities — police forces, customs services,
-- inspection authorities, courts, insurance regulators. An agency has a
-- TYPE (what it does) and optionally a REGION (where it operates). A
-- nationwide agency leaves region_id NULL.
CREATE TABLE agencies (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  region_id   UUID REFERENCES regions(id) ON DELETE SET NULL,
  code        TEXT NOT NULL,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL
               CHECK (type IN (
                 'police','traffic_police','customs','court',
                 'inspection','insurance','registry','license',
                 'audit','ministry','other'
               )),
  -- Contact + governance metadata: head_of_agency, phone, email,
  -- street address, regulator url. Schemaless to avoid prescribing
  -- one country's org chart globally.
  metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);
CREATE INDEX agencies_type_idx ON agencies (tenant_id, type);
CREATE INDEX agencies_region_idx ON agencies (tenant_id, region_id);

-- ============================================================================
-- Attribution columns on existing domain tables.
--
-- All NULLable; existing rows stay valid. New writes from updated services
-- will populate them. A later migration (after readers + writers are
-- migrated) will NOT NULL the columns we want to be required.
-- ============================================================================
ALTER TABLE officer_profiles
  ADD COLUMN agency_id UUID REFERENCES agencies(id) ON DELETE SET NULL;

ALTER TABLE fines
  ADD COLUMN issuing_agency_id UUID REFERENCES agencies(id) ON DELETE SET NULL,
  ADD COLUMN municipality_id   UUID REFERENCES municipalities(id) ON DELETE SET NULL;

ALTER TABLE vehicles
  ADD COLUMN registered_municipality_id UUID REFERENCES municipalities(id) ON DELETE SET NULL;

ALTER TABLE inspection_records
  ADD COLUMN agency_id UUID REFERENCES agencies(id) ON DELETE SET NULL;

ALTER TABLE insurance_records
  ADD COLUMN regulator_agency_id UUID REFERENCES agencies(id) ON DELETE SET NULL;

ALTER TABLE anpr_scans
  ADD COLUMN agency_id        UUID REFERENCES agencies(id) ON DELETE SET NULL,
  ADD COLUMN municipality_id  UUID REFERENCES municipalities(id) ON DELETE SET NULL;

-- ============================================================================
-- Row-Level Security: same tenant_isolation policy as the rest of the schema.
-- ============================================================================
DO $$
DECLARE t TEXT;
BEGIN
  FOR t IN SELECT unnest(ARRAY['regions','municipalities','agencies']) LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I
         USING (tenant_id = app_tenant())
         WITH CHECK (tenant_id = app_tenant())', t);
  END LOOP;
END $$;

-- ============================================================================
-- Demo data. The `demo` tenant is the development/CI baseline; seeding
-- one region + one municipality + one police agency lets every smoke
-- and integration test exercise the attribution path without each test
-- having to re-establish the geo hierarchy.
-- ============================================================================
INSERT INTO regions (tenant_id, code, name, metadata) VALUES
  ('demo', 'demo-central', 'Central Region',
   jsonb_build_object('iso_3166_2','XX-DC','population',5000000))
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO municipalities (tenant_id, region_id, code, name, kind, metadata)
SELECT 'demo', r.id, 'demo-capital', 'Demo City', 'city',
       jsonb_build_object('capital', true)
FROM regions r
WHERE r.tenant_id='demo' AND r.code='demo-central'
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO agencies (tenant_id, region_id, code, name, type, metadata)
SELECT 'demo', r.id, 'demo-police', 'Demo Traffic Police', 'traffic_police',
       jsonb_build_object('phone','+0000000000')
FROM regions r
WHERE r.tenant_id='demo' AND r.code='demo-central'
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO agencies (tenant_id, region_id, code, name, type, metadata)
VALUES ('demo', NULL, 'demo-registry', 'Demo Vehicle Registry', 'registry', '{}')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO agencies (tenant_id, region_id, code, name, type, metadata)
VALUES ('demo', NULL, 'demo-license', 'Demo Driver Licensing Authority', 'license', '{}')
ON CONFLICT (tenant_id, code) DO NOTHING;

-- Norway country pack (tenant 'no') exists from migration 0012. Seed the
-- equivalent: Oslo region + Oslo municipality + Politiet (national police).
INSERT INTO regions (tenant_id, code, name, metadata) VALUES
  ('no', 'no-03', 'Oslo',
   jsonb_build_object('iso_3166_2','NO-03','population',700000))
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO municipalities (tenant_id, region_id, code, name, kind, metadata)
SELECT 'no', r.id, 'no-0301', 'Oslo', 'city',
       jsonb_build_object('capital', true)
FROM regions r
WHERE r.tenant_id='no' AND r.code='no-03'
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO agencies (tenant_id, region_id, code, name, type, metadata)
VALUES ('no', NULL, 'no-politiet', 'Politiet', 'police', '{}')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO agencies (tenant_id, region_id, code, name, type, metadata)
VALUES ('no', NULL, 'no-statens-vegvesen', 'Statens Vegvesen', 'registry', '{}')
ON CONFLICT (tenant_id, code) DO NOTHING;
