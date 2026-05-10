-- ============================================================================
-- 0013_regions_municipalities_agencies.down.sql
--
-- Reverses 0013_up. Drops the attribution columns first (they FK into
-- the new tables) and then the new tables themselves. RLS policies are
-- dropped implicitly with the tables.
-- ============================================================================

ALTER TABLE anpr_scans
  DROP COLUMN IF EXISTS municipality_id,
  DROP COLUMN IF EXISTS agency_id;

ALTER TABLE insurance_records
  DROP COLUMN IF EXISTS regulator_agency_id;

ALTER TABLE inspection_records
  DROP COLUMN IF EXISTS agency_id;

ALTER TABLE vehicles
  DROP COLUMN IF EXISTS registered_municipality_id;

ALTER TABLE fines
  DROP COLUMN IF EXISTS municipality_id,
  DROP COLUMN IF EXISTS issuing_agency_id;

ALTER TABLE officer_profiles
  DROP COLUMN IF EXISTS agency_id;

DROP TABLE IF EXISTS agencies;
DROP TABLE IF EXISTS municipalities;
DROP TABLE IF EXISTS regions;
