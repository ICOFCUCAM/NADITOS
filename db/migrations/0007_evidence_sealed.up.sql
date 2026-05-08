-- Evidence retention reaper: mark a row "sealed" once the underlying
-- storage object has been deleted past its retention deadline. The
-- row stays for forensic continuity (sha256, size, taken_at, custody
-- chain) but the s3_key blob has been wiped from the bucket.
--
-- sealed_at NULL → still retrievable. NOT NULL → object gone.
-- Idempotency for the reaper relies on this: rows already sealed are
-- never reaped a second time.
ALTER TABLE fine_evidence
  ADD COLUMN IF NOT EXISTS sealed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS fine_evidence_unsealed_idx
  ON fine_evidence(tenant_id, sealed_at)
  WHERE sealed_at IS NULL;
