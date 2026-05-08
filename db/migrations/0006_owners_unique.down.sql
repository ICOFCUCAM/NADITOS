DROP INDEX IF EXISTS owners_tenant_user_uq;
DELETE FROM role_permissions
  WHERE permission IN ('owners:read','owners:write','owners:self');
