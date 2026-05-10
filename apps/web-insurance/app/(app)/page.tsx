"use client";

import { Card, Pill, SectionHeader, Stat, useSession } from "@naditos/web-common";

// Insurance partner overview. Shows the integration health from the
// provider's perspective — policy delivery rate, recent webhook
// failures, claim volume.

export default function InsuranceHome() {
  const { session } = useSession();
  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Provider integration"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Push policy lifecycle events; review delivery health."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Active policies"  value="—" />
        <Stat label="Sent today"       value="—" />
        <Stat label="Delivery rate (24h)" value="—" />
        <Stat label="Failed webhooks"  value="—" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Webhook health" title="Recent deliveries"
            description="Last 24 hours of provider-side requests." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            Connect to <code>GET /v1/insurance/webhooks/status</code> to populate.
          </p>
          <div className="mt-3"><Pill tone="ops">backend ready</Pill></div>
        </Card>
        <Card>
          <SectionHeader eyebrow="Documentation" title="Quick links"
            description="Integration guide, sample payloads, sandbox." />
          <ul className="text-sm text-[var(--fg-secondary)] mt-3 space-y-2">
            <li>POST <code>/v1/insurance/policies</code> — create or renew</li>
            <li>POST <code>/v1/insurance/claims</code> — file a claim</li>
            <li>GET  <code>/v1/insurance/policies/{`{id}`}</code> — lookup</li>
          </ul>
        </Card>
      </div>
    </div>
  );
}
