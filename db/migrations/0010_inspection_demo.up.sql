-- Demo vehicles with varied inspection / insurance / flag state so the
-- inspection-authority and audit consoles have something to render
-- without a 30-minute fixture-loading session.
--
-- Idempotent: ON CONFLICT (tenant_id, plate) DO UPDATE so re-running
-- after edits just refreshes the dates. Safe to apply to an
-- already-seeded environment.
--
-- Layout, by inspection state:
--   INSP-EXP-1, INSP-EXP-2  → overdue (negative offset)
--   INSP-DUE-1, INSP-DUE-2  → expiring within 30 days
--   INSP-OK-1               → fresh inspection, no concern
--
-- Plus two flagged vehicles so the "Flagged (red/black)" tile and the
-- compliance/inspection cards have content:
--   FLAG-RED-1   → expired insurance + expired inspection (red)
--   FLAG-BLACK-1 → marked stolen (black)

INSERT INTO vehicles (
  tenant_id, plate, make, model, year, category,
  inspection_expires_at, insurance_expires_at, registration_expires_at,
  is_stolen
)
VALUES
  -- Inspection: overdue.
  ('demo', 'INSP-EXP-1',  'Demo', 'Hatchback', 2010, 'car',
   now() - interval '15 days',  now() + interval '6 months',  now() + interval '1 year', false),
  ('demo', 'INSP-EXP-2',  'Demo', 'Sedan',     2012, 'car',
   now() - interval '3 days',   now() + interval '6 months',  now() + interval '1 year', false),

  -- Inspection: due within 30 days.
  ('demo', 'INSP-DUE-1',  'Demo', 'SUV',       2018, 'car',
   now() + interval '10 days',  now() + interval '6 months',  now() + interval '1 year', false),
  ('demo', 'INSP-DUE-2',  'Demo', 'Wagon',     2019, 'car',
   now() + interval '25 days',  now() + interval '6 months',  now() + interval '1 year', false),

  -- Inspection: ok.
  ('demo', 'INSP-OK-1',   'Demo', 'Compact',   2024, 'car',
   now() + interval '14 months', now() + interval '1 year',   now() + interval '1 year', false),

  -- Flagged red: expired insurance AND expired inspection.
  ('demo', 'FLAG-RED-1',  'Demo', 'Pickup',    2008, 'truck',
   now() - interval '40 days',  now() - interval '20 days',   now() + interval '6 months', false),

  -- Flagged black: stolen.
  ('demo', 'FLAG-BLACK-1','Demo', 'Coupe',     2020, 'car',
   now() + interval '6 months', now() + interval '6 months',  now() + interval '1 year', true)
ON CONFLICT (tenant_id, plate) DO UPDATE SET
  inspection_expires_at   = EXCLUDED.inspection_expires_at,
  insurance_expires_at    = EXCLUDED.insurance_expires_at,
  registration_expires_at = EXCLUDED.registration_expires_at,
  is_stolen               = EXCLUDED.is_stolen,
  updated_at              = now();
