DROP INDEX IF EXISTS fine_evidence_unsealed_idx;
ALTER TABLE fine_evidence DROP COLUMN IF EXISTS sealed_at;
