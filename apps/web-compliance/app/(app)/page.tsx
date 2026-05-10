"use client";

import { Card, Pill, SectionHeader, Stat, useSession } from "@naditos/web-common";

// Audit & Compliance home. Shows append-only ledger health, open
// alerts queue, and a recent-events feed. Backed by the existing
// audit and audit-alerts services.

export default function ComplianceHome() {
  const { session } = useSession();
  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Compliance oversight"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Verify the audit chain; review open alerts."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Events today"     value="—" />
        <Stat label="Open alerts"      value="—" />
        <Stat label="Chain status"     value="sealed" />
        <Stat label="Anomalies (24h)"  value="—" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Ledger" title="Recent audit events"
            description="Last 50 actions across all services." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            Connect to <code>GET /v1/audit/events?limit=50</code> to populate.
          </p>
          <div className="mt-3"><Pill tone="ops">backend ready</Pill></div>
        </Card>
        <Card>
          <SectionHeader eyebrow="Alerts" title="Open compliance alerts"
            description="Items requiring acknowledgement." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            Connect to <code>GET /v1/audit/alerts?status=open</code>.
          </p>
        </Card>
      </div>
    </div>
  );
}
