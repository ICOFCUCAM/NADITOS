-- Demo country pack so tenants have something to bind on first boot.
-- Real packs are reviewed by ministry legal teams and signed; this is
-- the minimal shape needed for development and CI.

INSERT INTO country_packs (id, country_code, version, effective_from, manifest)
VALUES (
  'demo-2026-01', 'XX', '1.0', '2026-01-01',
  $${
    "id":"demo-2026-01","country_code":"XX","version":"1.0",
    "effective_from":"2026-01-01",
    "locales":["en","fr","de","es","no","ar"],
    "currency":"EUR","plate_regex":"^[A-Z0-9-]{2,10}$",
    "offences":[
      {"code":"INS_EXPIRED","name":{"en":"Driving without valid insurance"},
       "base_amount":"400.00","currency":"EUR","points":4,
       "duplicate_window_min":1440,"rule_expr":"vehicle.insurance.expired"},
      {"code":"INSP_EXPIRED","name":{"en":"Driving without valid roadworthiness inspection"},
       "base_amount":"200.00","currency":"EUR","points":2,
       "duplicate_window_min":1440,"rule_expr":"vehicle.inspection.expired"},
      {"code":"REG_EXPIRED","name":{"en":"Expired vehicle registration"},
       "base_amount":"150.00","currency":"EUR","points":1,
       "duplicate_window_min":1440},
      {"code":"TAX_UNPAID","name":{"en":"Unpaid vehicle tax"},
       "base_amount":"100.00","currency":"EUR","points":0,
       "duplicate_window_min":1440},
      {"code":"PLATE_OBSCURED","name":{"en":"Obscured or illegible plate"},
       "base_amount":"80.00","currency":"EUR","points":1,
       "duplicate_window_min":60},
      {"code":"SEAT_BELT","name":{"en":"Seat belt not worn"},
       "base_amount":"90.00","currency":"EUR","points":2,
       "duplicate_window_min":60},
      {"code":"MOBILE_PHONE","name":{"en":"Mobile phone use while driving"},
       "base_amount":"150.00","currency":"EUR","points":3,
       "duplicate_window_min":60},
      {"code":"RED_LIGHT","name":{"en":"Running red light"},
       "base_amount":"250.00","currency":"EUR","points":4,
       "duplicate_window_min":60},
      {"code":"SPEED_30","name":{"en":"Speeding 30+ km/h over limit"},
       "base_amount":"500.00","currency":"EUR","points":6,
       "duplicate_window_min":60}
    ],
    "escalation":[
      {"stage":1,"after_days":7,"multiplier":1.0,"action":"warning"},
      {"stage":2,"after_days":14,"multiplier":1.5,"action":"penalty"},
      {"stage":3,"after_days":30,"multiplier":2.0,"action":"flag"},
      {"stage":4,"after_days":60,"multiplier":2.5,"action":"seize"},
      {"stage":5,"after_days":90,"multiplier":3.0,"action":"court"}
    ],
    "license_classes":[
      {"code":"A","name":{"en":"Motorcycles"},"min_age":18},
      {"code":"B","name":{"en":"Cars"},"min_age":18},
      {"code":"C","name":{"en":"Heavy goods"},"min_age":21,"max_weight_kg":40000},
      {"code":"D","name":{"en":"Buses"},"min_age":24}
    ],
    "vehicle_categories":[
      {"code":"car","inspection_months":24},
      {"code":"motorcycle","inspection_months":24},
      {"code":"truck","inspection_months":12},
      {"code":"bus","inspection_months":6}
    ]
  }$$::jsonb
) ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_country_pack (tenant_id, pack_id)
VALUES ('demo', 'demo-2026-01')
ON CONFLICT (tenant_id) DO UPDATE SET pack_id=EXCLUDED.pack_id, applied_at=now();

INSERT INTO driver_demerit_policy (tenant_id, threshold_points, window_months, suspension_months, reset_after_months)
VALUES ('demo', 12, 24, 6, 36)
ON CONFLICT (tenant_id) DO NOTHING;

INSERT INTO evidence_retention_policy (tenant_id, default_days, paid_fine_days, cancelled_fine_days)
VALUES ('demo', 1825, 1825, 365)
ON CONFLICT (tenant_id) DO NOTHING;
