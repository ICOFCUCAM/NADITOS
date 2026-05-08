-- vehicle_transfers: citizen-to-citizen ownership transfer with a
-- short-lived transfer code. The seller initiates the transfer; the
-- buyer accepts by entering the code. Outstanding fines stay attached
-- to the seller (the fine's driver_user_id and the existing owner_id
-- snapshot in fines events are unchanged) — only the *future*
-- responsibility shifts.
--
-- Lifecycle:
--   pending   ──accept──▶ accepted   (owner_id flipped)
--             ──cancel──▶ cancelled  (seller pulled the offer)
--             ──expire──▶ expired    (no one accepted in 7 days)
--
-- One vehicle can only have one OPEN transfer at a time — the
-- partial unique index below enforces that. Once cancelled or
-- expired, the seller can start a fresh transfer.

CREATE TABLE vehicle_transfers (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  vehicle_id    UUID NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  from_owner    UUID NOT NULL REFERENCES owners(id),
  to_owner      UUID REFERENCES owners(id),
  to_contact    TEXT NOT NULL,
  code          TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at    TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days'),
  accepted_at   TIMESTAMPTZ,
  CONSTRAINT vehicle_transfers_status_check
    CHECK (status IN ('pending','accepted','cancelled','expired'))
);

-- One open transfer per vehicle. After the row hits a terminal status
-- the seller can issue a fresh transfer.
CREATE UNIQUE INDEX vehicle_transfers_one_open
  ON vehicle_transfers(tenant_id, vehicle_id)
  WHERE status = 'pending';

-- Code is unique within a tenant so the buyer just needs to type it.
-- Codes are short (6 chars) so this index also catches collisions on
-- generation; the handler retries on conflict.
CREATE UNIQUE INDEX vehicle_transfers_code
  ON vehicle_transfers(tenant_id, code)
  WHERE status = 'pending';

CREATE INDEX vehicle_transfers_from_idx
  ON vehicle_transfers(tenant_id, from_owner, created_at DESC);

ALTER TABLE vehicle_transfers ENABLE ROW LEVEL SECURITY;
ALTER TABLE vehicle_transfers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON vehicle_transfers
  USING (tenant_id = app_tenant())
  WITH CHECK (tenant_id = app_tenant());
