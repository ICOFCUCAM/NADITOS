-- 0011 — broaden the `demo` tenant's plate_regex.
--
-- Now that the registry validates POST /v1/vehicles plate against
-- the tenant's plate_regex (and not just the schema CHECK constraint),
-- the original `^[A-Z0-9-]{2,10}$` rejects fixture plates like
-- FLAG-BLACK-1 (12 chars) that we already inserted via migration
-- 0010. Existing rows aren't re-validated, but future creates would
-- 400. Bump the demo tenant up to 12 chars so the demo seed and any
-- future demo additions of the same shape stay valid.
--
-- This affects only tenant 'demo'. Production country packs land
-- their own per-jurisdiction regex via tenant_country_pack /
-- ministry admin tooling and are unaffected.

UPDATE tenants
   SET plate_regex = '^[A-Z0-9-]{2,12}$'
 WHERE id = 'demo'
   AND plate_regex = '^[A-Z0-9-]{2,10}$';
