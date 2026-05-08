-- Citizen self-claim relies on a unique (tenant_id, user_id) so calling
-- POST /v1/citizens/me/owner repeatedly upserts instead of creating
-- duplicates. user_id is nullable for owners without a portal account.
--
-- Older databases may carry duplicate (tenant_id, user_id) pairs left
-- by previous smoke runs that hit the SQL workaround. We collapse them
-- by keeping the earliest row per pair and re-pointing dependent
-- vehicles to the survivor before adding the constraint.
WITH ranked AS (
  SELECT id, tenant_id, user_id, created_at,
         ROW_NUMBER() OVER (PARTITION BY tenant_id, user_id ORDER BY created_at, id) AS rn
    FROM owners
   WHERE user_id IS NOT NULL
), survivor AS (
  SELECT tenant_id, user_id, id AS keep_id FROM ranked WHERE rn = 1
), loser AS (
  SELECT id FROM ranked WHERE rn > 1
)
UPDATE vehicles v
   SET owner_id = s.keep_id
  FROM owners o, survivor s
 WHERE v.owner_id = o.id
   AND s.tenant_id = o.tenant_id
   AND s.user_id   = o.user_id
   AND o.id IN (SELECT id FROM loser);

DELETE FROM owners
 WHERE id IN (
   SELECT id FROM (
     SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id, user_id ORDER BY created_at, id) AS rn
       FROM owners WHERE user_id IS NOT NULL
   ) d WHERE rn > 1
 );

CREATE UNIQUE INDEX IF NOT EXISTS owners_tenant_user_uq
  ON owners(tenant_id, user_id)
  WHERE user_id IS NOT NULL;

-- Permission additions so admins can manage owners and citizens can
-- self-claim. The seed migration added the basic perms; this layer is
-- additive and idempotent.
INSERT INTO role_permissions (tenant_id, role_code, permission)
SELECT t.id AS tenant_id, p.role_code, p.perm
  FROM tenants t
  CROSS JOIN (VALUES
    ('admin',   'owners:read'),
    ('admin',   'owners:write'),
    ('officer', 'owners:read'),
    ('citizen', 'owners:self')
  ) AS p(role_code, perm)
  WHERE EXISTS (
    SELECT 1 FROM roles r
     WHERE r.tenant_id = t.id AND r.code = p.role_code
  )
ON CONFLICT DO NOTHING;
