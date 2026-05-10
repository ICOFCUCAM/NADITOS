-- Reverse 0012_country_pack_no: drop the NO tenant and all rows that
-- only exist because of it. The country pack itself is kept (other
-- tenants might come to reference it) — drop it manually if not.

DELETE FROM vehicles                  WHERE tenant_id = 'no';
DELETE FROM user_roles                WHERE tenant_id = 'no';
DELETE FROM users                     WHERE tenant_id = 'no';
DELETE FROM regulation_escalation     WHERE tenant_id = 'no';
DELETE FROM regulation_offences       WHERE tenant_id = 'no';
DELETE FROM role_permissions          WHERE tenant_id = 'no';
DELETE FROM roles                     WHERE tenant_id = 'no';
DELETE FROM tenant_country_pack       WHERE tenant_id = 'no';
DELETE FROM tenants                   WHERE id        = 'no';
-- DROP DATABASE-style: leaves country_packs/no-2026-01 in place.
