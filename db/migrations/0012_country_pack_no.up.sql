-- 0012 — Norway country pack and tenant.
--
-- Demonstrates the multi-tenant story: a separate jurisdiction with
-- its own plate format (^[A-Z]{2}\s\d{5}$, e.g. "AB 12345"), currency
-- (NOK), locale (no-NO), and offence catalogue. The 'no' tenant ships
-- with the same role taxonomy as 'demo' (admin / officer / citizen /
-- court) so role-based UI logic ports without changes; the data is
-- the differentiator.
--
-- Idempotent throughout. Safe to re-run on an environment that
-- already has the country pack.

-- ─── 1. Country pack manifest ──────────────────────────────────────
INSERT INTO country_packs (id, country_code, version, effective_from, manifest)
VALUES (
  'no-2026-01', 'NO', '1.0', '2026-01-01',
  $${
    "id":"no-2026-01","country_code":"NO","version":"1.0",
    "effective_from":"2026-01-01",
    "locales":["no","en"],
    "currency":"NOK","plate_regex":"^[A-Z]{2}\\s\\d{5}$",
    "offences":[
      {"code":"INS_EXPIRED","name":{"no":"Manglende gyldig forsikring","en":"Driving without valid insurance"},
       "base_amount":"5500.00","currency":"NOK","points":4,"duplicate_window_min":1440,
       "rule_expr":"vehicle.insurance.expired"},
      {"code":"INSP_EXPIRED","name":{"no":"Manglende EU-kontroll","en":"Driving without valid roadworthiness inspection"},
       "base_amount":"3000.00","currency":"NOK","points":2,"duplicate_window_min":1440,
       "rule_expr":"vehicle.inspection.expired"},
      {"code":"REG_EXPIRED","name":{"no":"Utgått registrering","en":"Expired vehicle registration"},
       "base_amount":"2500.00","currency":"NOK","points":1,"duplicate_window_min":1440},
      {"code":"PLATE_OBSCURED","name":{"no":"Skjult eller uleselig nummerskilt","en":"Obscured or illegible plate"},
       "base_amount":"1100.00","currency":"NOK","points":1,"duplicate_window_min":60},
      {"code":"SEAT_BELT","name":{"no":"Manglende bilbeltebruk","en":"Seat belt not worn"},
       "base_amount":"1500.00","currency":"NOK","points":2,"duplicate_window_min":60},
      {"code":"MOBILE_PHONE","name":{"no":"Bruk av håndholdt mobil under kjøring","en":"Mobile phone use while driving"},
       "base_amount":"5000.00","currency":"NOK","points":3,"duplicate_window_min":60},
      {"code":"RED_LIGHT","name":{"no":"Kjøring mot rødt lys","en":"Running red light"},
       "base_amount":"6800.00","currency":"NOK","points":4,"duplicate_window_min":60},
      {"code":"SPEED_30","name":{"no":"Hastighetsoverskridelse 30+ km/t","en":"Speeding 30+ km/h over limit"},
       "base_amount":"10400.00","currency":"NOK","points":6,"duplicate_window_min":60}
    ],
    "escalation":[
      {"stage":1,"after_days":21,"multiplier":1.0,"action":"warning"},
      {"stage":2,"after_days":42,"multiplier":1.5,"action":"penalty"},
      {"stage":3,"after_days":90,"multiplier":2.0,"action":"flag"},
      {"stage":4,"after_days":180,"multiplier":2.5,"action":"seize"},
      {"stage":5,"after_days":365,"multiplier":3.0,"action":"court"}
    ]
  }$$::jsonb
) ON CONFLICT (id) DO NOTHING;

-- ─── 2. Tenant + country binding ───────────────────────────────────
INSERT INTO tenants (id, name, country_code, default_locale, currency, plate_regex, modules)
VALUES ('no', 'Norge — Vegvesen', 'NO', 'no', 'NOK',
        '^[A-Z]{2}\s\d{5}$',
        '{"registry":true,"fines":true,"license":true,"insurance":true,"inspection":true,"anpr":true}')
ON CONFLICT (id) DO UPDATE SET
  plate_regex   = EXCLUDED.plate_regex,
  currency      = EXCLUDED.currency,
  default_locale= EXCLUDED.default_locale,
  modules       = EXCLUDED.modules;

INSERT INTO tenant_country_pack (tenant_id, pack_id)
VALUES ('no', 'no-2026-01')
ON CONFLICT (tenant_id) DO UPDATE SET pack_id=EXCLUDED.pack_id, applied_at=now();

-- ─── 3. Roles + permissions (mirrors 'demo' for portability) ───────
INSERT INTO roles (tenant_id, code, name) VALUES
  ('no','admin',  'Ministerieadministrator'),
  ('no','officer','Politibetjent'),
  ('no','citizen','Borger'),
  ('no','court',  'Domstolsfullmektig')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (tenant_id, role_code, permission) VALUES
  ('no','admin','registry:read'),     ('no','admin','registry:write'),
  ('no','admin','fines:read'),        ('no','admin','fines:cancel'),
  ('no','admin','license:read'),      ('no','admin','license:write'),
  ('no','admin','audit:read'),        ('no','admin','users:write'),
  ('no','admin','regulation:write'),
  ('no','officer','registry:read'),   ('no','officer','license:read'),
  ('no','officer','fines:create'),    ('no','officer','fines:read'),
  ('no','officer','anpr:scan'),
  ('no','citizen','self:read'),       ('no','citizen','fines:pay'),
  ('no','citizen','fines:dispute'),
  ('no','court','fines:read'),        ('no','court','fines:judge')
ON CONFLICT DO NOTHING;

-- ─── 4. Offences (also denormalised onto regulation_offences) ──────
INSERT INTO regulation_offences
  (tenant_id, code, name, base_amount, currency, rule_expr, duplicate_window_min)
VALUES
  ('no','INS_EXPIRED',    '{"no":"Manglende gyldig forsikring","en":"Driving without valid insurance"}',
     5500.00,  'NOK', 'vehicle.insurance.expired', 1440),
  ('no','INSP_EXPIRED',   '{"no":"Manglende EU-kontroll","en":"Driving without valid roadworthiness inspection"}',
     3000.00,  'NOK', 'vehicle.inspection.expired', 1440),
  ('no','REG_EXPIRED',    '{"no":"Utgått registrering","en":"Expired vehicle registration"}',
     2500.00,  'NOK', 'vehicle.registration.expired', 1440),
  ('no','PLATE_OBSCURED', '{"no":"Skjult eller uleselig nummerskilt","en":"Obscured or illegible plate"}',
     1100.00,  'NOK', NULL, 60),
  ('no','SEAT_BELT',      '{"no":"Manglende bilbeltebruk","en":"Seat belt not worn"}',
     1500.00,  'NOK', NULL, 60),
  ('no','MOBILE_PHONE',   '{"no":"Bruk av håndholdt mobil under kjøring","en":"Mobile phone use while driving"}',
     5000.00,  'NOK', NULL, 60),
  ('no','RED_LIGHT',      '{"no":"Kjøring mot rødt lys","en":"Running red light"}',
     6800.00,  'NOK', NULL, 60),
  ('no','SPEED_30',       '{"no":"Hastighetsoverskridelse 30+ km/t","en":"Speeding 30+ km/h over limit"}',
    10400.00,  'NOK', NULL, 60)
ON CONFLICT DO NOTHING;

INSERT INTO regulation_escalation (tenant_id, stage, after_days, multiplier, action) VALUES
  ('no',1,  21, 1.0, 'warning'),
  ('no',2,  42, 1.5, 'penalty'),
  ('no',3,  90, 2.0, 'flag'),
  ('no',4, 180, 2.5, 'seize'),
  ('no',5, 365, 3.0, 'court')
ON CONFLICT DO NOTHING;

-- ─── 5. Demo users for the NO tenant ───────────────────────────────
INSERT INTO users (tenant_id, email, password_hash, full_name, is_active) VALUES
  ('no', 'admin@no',   crypt('demo1234', gen_salt('bf', 10)), 'Norway admin',   true),
  ('no', 'officer@no', crypt('demo1234', gen_salt('bf', 10)), 'Norway officer', true),
  ('no', 'citizen@no', crypt('demo1234', gen_salt('bf', 10)), 'Norway citizen', true)
ON CONFLICT (tenant_id, email) DO UPDATE SET password_hash = EXCLUDED.password_hash;

INSERT INTO user_roles (tenant_id, user_id, role_code)
SELECT 'no', u.id,
  CASE u.email::TEXT
    WHEN 'admin@no'   THEN 'admin'
    WHEN 'officer@no' THEN 'officer'
    WHEN 'citizen@no' THEN 'citizen'
  END
FROM users u
WHERE u.tenant_id = 'no'
  AND u.email::TEXT IN ('admin@no','officer@no','citizen@no')
ON CONFLICT DO NOTHING;

-- ─── 6. A handful of realistic NO plates ───────────────────────────
INSERT INTO vehicles (
  tenant_id, plate, make, model, year, category,
  inspection_expires_at, insurance_expires_at, registration_expires_at,
  is_stolen
)
VALUES
  ('no', 'AB 12345', 'Volvo',  'XC60',    2022, 'car',
   now() + interval '8 months',  now() + interval '6 months',  now() + interval '2 years', false),
  ('no', 'CD 67890', 'Tesla',  'Model 3', 2023, 'car',
   now() + interval '6 months',  now() + interval '4 months',  now() + interval '1 year',  false),
  ('no', 'EF 11111', 'Toyota', 'RAV4',    2019, 'car',
   now() + interval '20 days',   now() + interval '6 months',  now() + interval '1 year',  false),
  ('no', 'GH 22222', 'Ford',   'Transit', 2017, 'truck',
   now() - interval '15 days',   now() + interval '6 months',  now() + interval '1 year',  false),
  ('no', 'IJ 99999', 'BMW',    'X5',      2020, 'car',
   now() + interval '6 months',  now() + interval '6 months',  now() + interval '1 year',  true)
ON CONFLICT (tenant_id, plate) DO UPDATE SET
  inspection_expires_at = EXCLUDED.inspection_expires_at,
  insurance_expires_at  = EXCLUDED.insurance_expires_at,
  is_stolen             = EXCLUDED.is_stolen;
