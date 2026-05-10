"use client";

import { Card, Pill, SectionHeader, Stat, useSession } from "@naditos/web-common";

// Inspection station home. Placeholder until the inspection service
// exposes its summary endpoints; the structure here matches the
// admin/police home so the visual language stays consistent.

export default function InspectionHome() {
  const { session } = useSession();
  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Today at this station"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Schedule, run, and certify vehicle inspections."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Scheduled today" value="—" />
        <Stat label="Completed today" value="—" />
        <Stat label="Pass rate (7d)" value="—" />
        <Stat label="Open re-inspections" value="—" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Queue" title="Next vehicles in"
            description="Walk-in registrations and scheduled appointments." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            No vehicles in queue. Use the <strong>Queue</strong> tab in the
            sidebar to add an arrival.
          </p>
        </Card>
        <Card>
          <SectionHeader eyebrow="Recent inspections" title="Last completed"
            description="Past 24 hours, this station." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            Connect to <code>GET /v1/inspection/records</code> from the
            inspection service to populate this list.
          </p>
          <div className="mt-3"><Pill tone="ops">backend ready</Pill></div>
        </Card>
      </div>
    </div>
  );
}
