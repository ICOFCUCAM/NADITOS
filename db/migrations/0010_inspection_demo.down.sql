-- Reverse 0010_inspection_demo: drop the demo vehicles inserted there.
-- Other tenants' rows and any non-demo plates are untouched.

DELETE FROM vehicles
 WHERE tenant_id = 'demo'
   AND plate IN (
     'INSP-EXP-1','INSP-EXP-2',
     'INSP-DUE-1','INSP-DUE-2',
     'INSP-OK-1',
     'FLAG-RED-1','FLAG-BLACK-1'
   );
