"use client";

import { useEffect, useState } from "react";
import {
  Card, Pill, SectionHeader, Skeleton, Stat,
  services, useSession,
} from "@naditos/web-common";

// Audit & Compliance home. Pulls live data from the audit service:
//
//   GET /v1/audit/events?limit=50           — append-only ledger
//   GET /v1/audit/alerts?status=open        — open compliance alerts
//   GET /v1/audit/verify                    — hash-chain verification
//
// All three need NeedsRole=admin at the gateway, so the operator must
// log in as admin@demo (or any user with role=admin) to populate the
// page; the gateway returns 403 otherwise and the dashboard simply
// shows an em-dash for those tiles.

type AuditEvent = {
  id: string;
  occurred_at: string;
  service: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  actor_role?: string;
};
type Alert = {
  id: string;
  severity?: "low" | "medium" | "high";
  status?: string;
  created_at?: string;
  reason?: string;
};
type Verify = { ok: boolean; checked: number };

export default function ComplianceHome() {
  const { session } = useSession();
  const [events, setEvents] = useState<AuditEvent[] | null>(null);
  const [alerts, setAlerts] = useState<Alert[] | null>(null);
  const [verify, setVerify] = useState<Verify | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!session) return;
    setLoading(true);
    const auth = { token: session.accessToken, tenant: session.user.tenant };
    Promise.all([
      services.audit(`/v1/audit/events?limit=50&tenant_id=${session.user.tenant}`, auth)
        .catch(() => null) as Promise<{ items: AuditEvent[] } | null>,
      services.audit(`/v1/audit/alerts?status=open`, auth)
        .catch(() => null) as Promise<{ items: Alert[] } | null>,
      services.audit(`/v1/audit/verify?tenant_id=${session.user.tenant}`, auth)
        .catch(() => null) as Promise<Verify | null>,
    ]).then(([e, a, v]) => {
      setEvents(e?.items ?? []);
      setAlerts(a?.items ?? []);
      setVerify(v);
    }).finally(() => setLoading(false));
  }, [session]);

  const today = new Date().toISOString().slice(0, 10);
  const eventsToday = events?.filter((x) => x.occurred_at?.startsWith(today)) ?? [];
  const highAlerts = alerts?.filter((a) => a.severity === "high") ?? [];

  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Compliance oversight"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Verify the audit chain; review open alerts."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Events today"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : eventsToday.length} />
        <Stat label="Open alerts"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : (alerts?.length ?? "—")} />
        <Stat label="Chain status"
              value={loading ? <Skeleton className="h-4 w-20 inline-block" /> :
                     verify === null ? "—" :
                     verify.ok ? `sealed (${verify.checked})` : "BROKEN"} />
        <Stat label="High-severity alerts"
              value={loading ? <Skeleton className="h-4 w-16 inline-block" /> : (highAlerts.length || 0)} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Ledger" title="Recent audit events"
            description={loading ? "…" :
              events && events.length > 0
                ? `Last ${events.length} actions across all services.`
                : "Connect via /v1/audit/events — admin role required."} />
          {loading ? (
            <div className="mt-3 space-y-2">
              {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}
            </div>
          ) : events && events.length > 0 ? (
            <ul className="mt-3 space-y-1 text-sm">
              {events.slice(0, 8).map((e) => (
                <li key={e.id} className="flex justify-between gap-3
                                          py-1 border-b border-[var(--border-subtle)] last:border-0">
                  <span className="text-[var(--fg-secondary)] truncate">
                    <span className="font-medium text-[var(--fg-primary)]">{e.service}</span>
                    {" · "}{e.action}{e.resource_type ? ` · ${e.resource_type}` : ""}
                  </span>
                  <span className="text-[var(--fg-muted)] text-xs whitespace-nowrap">
                    {timeAgo(e.occurred_at)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-[var(--fg-muted)] mt-3">No events visible.</p>
          )}
        </Card>

        <Card>
          <SectionHeader eyebrow="Alerts" title="Open compliance alerts"
            description={loading ? "…" :
              alerts && alerts.length > 0
                ? `${alerts.length} item${alerts.length === 1 ? "" : "s"} requiring acknowledgement.`
                : "All clear."} />
          {loading ? (
            <div className="mt-3 space-y-2">
              {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-5 w-full" />)}
            </div>
          ) : alerts && alerts.length > 0 ? (
            <ul className="mt-3 space-y-1 text-sm">
              {alerts.slice(0, 6).map((a) => (
                <li key={a.id} className="flex items-center justify-between gap-3
                                          py-1 border-b border-[var(--border-subtle)] last:border-0">
                  <span className="text-[var(--fg-secondary)] truncate">{a.reason ?? a.id}</span>
                  <Pill tone={a.severity === "high" ? "red" : a.severity === "medium" ? "amber" : "slate"}>
                    {a.severity ?? "?"}
                  </Pill>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-[var(--fg-muted)] mt-3">No open alerts.</p>
          )}
        </Card>
      </div>
    </div>
  );
}

function timeAgo(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso).getTime();
  const s = Math.max(0, (Date.now() - d) / 1000);
  if (s < 60)   return `${Math.floor(s)}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400)return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
