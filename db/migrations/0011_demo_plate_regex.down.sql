-- Reverse 0011: restore the original {2,10} regex on the demo tenant.

UPDATE tenants
   SET plate_regex = '^[A-Z0-9-]{2,10}$'
 WHERE id = 'demo'
   AND plate_regex = '^[A-Z0-9-]{2,12}$';
