-- Demo seed: tenant 'demo', a few roles + permissions, two users.
-- Run AS naditos_admin (which has row_security off) so RLS doesn't block.

INSERT INTO tenants (id, name, country_code, default_locale, currency, plate_regex, modules)
VALUES ('demo', 'Demo Republic', 'XX', 'en', 'EUR', '^[A-Z0-9-]{2,10}$',
        '{"registry":true,"fines":true,"license":true,"insurance":true,"inspection":true,"anpr":true}')
ON CONFLICT (id) DO NOTHING;

INSERT INTO roles (tenant_id, code, name) VALUES
  ('demo','admin',  'Ministry administrator'),
  ('demo','officer','Police officer'),
  ('demo','citizen','Citizen'),
  ('demo','court',  'Court clerk')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_code, permission) VALUES
  -- admin: everything readable + admin actions
  ('demo','admin','registry:read'),    ('demo','admin','registry:write'),
  ('demo','admin','fines:read'),       ('demo','admin','fines:cancel'),
  ('demo','admin','license:read'),     ('demo','admin','license:write'),
  ('demo','admin','audit:read'),       ('demo','admin','users:write'),
  ('demo','admin','regulation:write'),
  -- officer: read registry/license, create fines
  ('demo','officer','registry:read'),  ('demo','officer','license:read'),
  ('demo','officer','fines:create'),   ('demo','officer','fines:read'),
  ('demo','officer','anpr:scan'),
  -- citizen: read/pay own
  ('demo','citizen','self:read'),      ('demo','citizen','fines:pay'),
  ('demo','citizen','fines:dispute'),
  -- court
  ('demo','court','fines:read'),       ('demo','court','fines:judge')
ON CONFLICT DO NOTHING;

-- Demo offences
INSERT INTO regulation_offences
  (tenant_id, code, name, base_amount, currency, rule_expr, duplicate_window_min)
VALUES
  ('demo','INS_EXPIRED',
     '{"en":"Driving without valid insurance","fr":"Défaut d''assurance"}',
     400.00, 'EUR', 'vehicle.insurance.expired', 1440),
  ('demo','INSP_EXPIRED',
     '{"en":"Driving without valid roadworthiness inspection","fr":"Contrôle technique expiré"}',
     200.00, 'EUR', 'vehicle.inspection.expired', 1440),
  ('demo','REG_EXPIRED',
     '{"en":"Expired vehicle registration"}',
     150.00, 'EUR', 'vehicle.registration.expired', 1440),
  ('demo','TAX_UNPAID',
     '{"en":"Unpaid vehicle tax"}',
     100.00, 'EUR', 'vehicle.tax.unpaid', 1440),
  ('demo','PLATE_OBSCURED',
     '{"en":"Obscured or illegible plate"}',
      80.00, 'EUR', NULL, 60),
  ('demo','SEAT_BELT',
     '{"en":"Driver/passenger not wearing seat belt"}',
      90.00, 'EUR', NULL, 60),
  ('demo','MOBILE_PHONE',
     '{"en":"Use of mobile phone while driving"}',
     150.00, 'EUR', NULL, 60),
  ('demo','RED_LIGHT',
     '{"en":"Running red light"}',
     250.00, 'EUR', NULL, 60),
  ('demo','SPEED_30',
     '{"en":"Speeding 30+ km/h over limit"}',
     500.00, 'EUR', NULL, 60)
ON CONFLICT DO NOTHING;

INSERT INTO regulation_escalation (tenant_id, stage, after_days, multiplier, action) VALUES
  ('demo',1,  7, 1.0,  'warning'),
  ('demo',2, 14, 1.5,  'penalty'),
  ('demo',3, 30, 2.0,  'flag'),
  ('demo',4, 60, 2.5,  'seize'),
  ('demo',5, 90, 3.0,  'court')
ON CONFLICT DO NOTHING;
