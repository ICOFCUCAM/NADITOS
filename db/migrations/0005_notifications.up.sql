-- ============================================================================
-- NADITOS Phase-3: notifications consumer + outbox offset tracking.
-- ============================================================================

-- Each independent consumer of event_outbox tracks its own progress here.
-- The fines/registry relays use the delivered_at column on event_outbox
-- (which means "forwarded to the canonical bus"); this offset table is
-- for in-process consumers that read the outbox directly without
-- competing with the relay.
CREATE TABLE event_consumer_offsets (
  consumer       TEXT PRIMARY KEY,             -- e.g. 'notifications', 'analytics'
  last_event_id  BIGINT NOT NULL DEFAULT 0,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Notification records — one row per outbound message attempt. Holds
-- the rendered body, the chosen channel, the resolved recipient, and
-- the provider's reference once delivered.
CREATE TYPE notification_status AS ENUM ('pending','sent','failed','suppressed');

CREATE TABLE notification_records (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  related_event   BIGINT,                       -- event_outbox.id that triggered this
  channel         TEXT NOT NULL CHECK (channel IN ('sms','email','push')),
  recipient       TEXT NOT NULL,                -- resolved address
  subject         TEXT,                         -- email only
  body            TEXT NOT NULL,
  template        TEXT,                         -- template id for analytics
  status          notification_status NOT NULL DEFAULT 'pending',
  provider        TEXT,                         -- adapter Info().Provider
  provider_ref    TEXT,                         -- upstream message id
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at         TIMESTAMPTZ
);
CREATE INDEX notification_records_tenant_idx ON notification_records(tenant_id, created_at DESC);
CREATE INDEX notification_records_event_idx ON notification_records(related_event);

-- RLS for the new tables.
DO $$
DECLARE t TEXT;
BEGIN
  FOR t IN SELECT unnest(ARRAY['event_consumer_offsets','notification_records']) LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  END LOOP;
END $$;

-- event_consumer_offsets is global (not per-tenant) — the offset is one
-- counter per consumer name. Allow access only to BYPASSRLS roles.
CREATE POLICY consumer_offsets_admin_only
  ON event_consumer_offsets
  USING (false)
  WITH CHECK (false);

-- notification_records IS per-tenant.
CREATE POLICY tenant_isolation
  ON notification_records
  USING (tenant_id = app_tenant())
  WITH CHECK (tenant_id = app_tenant());
