"use client";

import { useEffect, useState } from "react";
import {
  Card, Pill, SectionHeader, Skeleton, Stat,
  services, useSession,
} from "@naditos/web-common";

// Inspection station home. The inspection service is currently a
// background worker without an HTTP API surface, so this page sources
// its numbers from the registry (the source of truth for vehicle
// compliance state):
//
//   GET /v1/vehicles            — flagged vehicles, status counts
//   GET /v1/vehicles?flagged=1  — only red/black entries
//
// When the inspection service grows a /v1/inspection/queue endpoint
// (scheduled appointments at this station), wire it in alongside the
// registry data.

type Vehicle = {
  id: string;
  plate: string;
  make?: string;
  model?: string;
  status?: "green" | "yellow" | "red" | "black";
  inspection_expires_at?: string | null;
};

export default function InspectionHome() {
  const { session } = useSession();
  const [vehicles, setVehicles] = useState<Vehicle[] | null>(null);
  const [flagged, setFlagged] = useState<Vehicle[] | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!session) return;
    setLoading(true);
    const auth = { token: session.accessToken, tenant: session.user.tenant };
    Promise.all([
      services.registry(`/v1/vehicles`,         auth).catch(() => null) as Promise<{ items: Vehicle[] } | null>,
      services.registry(`/v1/vehicles?flagged=1`, auth).catch(() => null) as Promise<{ items: Vehicle[] } | null>,
    ]).then(([all, flag]) => {
      setVehicles(all?.items ?? []);
      setFlagged(flag?.items ?? []);
    }).finally(() => setLoading(false));
  }, [session]);

  const dueSoon = (vehicles ?? []).filter((v) => {
    if (!v.inspection_expires_at) return false;
    const t = new Date(v.inspection_expires_at).getTime();
    const days = (t - Date.now()) / 86400000;
    return days >= 0 && days <= 30;
  });
  const overdue = (vehicles ?? []).filter((v) => {
    if (!v.inspection_expires_at) return false;
    return new Date(v.inspection_expires_at).getTime() < Date.now();
  });

  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Today at this station"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Schedule, run, and certify vehicle inspections."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Vehicles registered"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : (vehicles?.length ?? "—")} />
        <Stat label="Inspection due (≤30d)"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : dueSoon.length} />
        <Stat label="Overdue"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : overdue.length} />
        <Stat label="Flagged (red/black)"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : (flagged?.length ?? "—")} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Inspection due" title="Upcoming expirations"
            description={loading ? "…" :
              dueSoon.length > 0
                ? `${dueSoon.length} vehicle${dueSoon.length === 1 ? "" : "s"} expire within 30 days.`
                : "No vehicles expiring in the next 30 days."} />
          {loading ? (
            <div className="mt-3 space-y-2">
              {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}
            </div>
          ) : dueSoon.length > 0 ? (
            <ul className="mt-3 space-y-1 text-sm">
              {dueSoon.slice(0, 8).map((v) => (
                <li key={v.id} className="flex justify-between gap-3
                                          py-1 border-b border-[var(--border-subtle)] last:border-0">
                  <span className="text-[var(--fg-primary)] font-mono">{v.plate}</span>
                  <span className="text-[var(--fg-muted)] text-xs whitespace-nowrap">
                    {v.inspection_expires_at ? new Date(v.inspection_expires_at).toLocaleDateString() : "—"}
                  </span>
                </li>
              ))}
            </ul>
          ) : null}
        </Card>

        <Card>
          <SectionHeader eyebrow="Flagged" title="Compliance attention"
            description={loading ? "…" :
              flagged && flagged.length > 0
                ? `${flagged.length} vehicle${flagged.length === 1 ? "" : "s"} with red/black status.`
                : "No flagged vehicles in this jurisdiction."} />
          {loading ? (
            <div className="mt-3 space-y-2">
              {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}
            </div>
          ) : flagged && flagged.length > 0 ? (
            <ul className="mt-3 space-y-1 text-sm">
              {flagged.slice(0, 6).map((v) => (
                <li key={v.id} className="flex items-center justify-between gap-3
                                          py-1 border-b border-[var(--border-subtle)] last:border-0">
                  <span className="text-[var(--fg-primary)] font-mono">{v.plate}</span>
                  <Pill tone={v.status === "black" ? "black" : v.status === "red" ? "red" : "amber"}>
                    {v.status ?? "?"}
                  </Pill>
                </li>
              ))}
            </ul>
          ) : null}
        </Card>
      </div>
    </div>
  );
}
