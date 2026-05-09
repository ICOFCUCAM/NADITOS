"use client";

// Admin notifications log.
//
// Most outbound messages flow through the consumer that drains
// event_outbox (fine.issued, fine.cancelled, vehicle.flagged, …).
// notification_records is where every send attempt lands — pending,
// sent, failed, suppressed (rate-limit / quiet-hours). This page
// surfaces that table so admins can spot delivery issues without
// shelling into the database.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Item = {
  id: string;
  channel: string;
  recipient: string;
  subject: string;
  template: string | null;
  status: string;
  provider: string;
  provider_ref: string;
  created_at: string;
  sent_at: string | null;
};

const FILTERS = ["all", "sent", "pending", "failed", "suppressed"] as const;
type Filter = typeof FILTERS[number];

const TONE: Record<string, "green" | "amber" | "red" | "slate"> = {
  sent: "green",
  pending: "amber",
  failed: "red",
  suppressed: "slate",
};

export default function NotificationsPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Item[] | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!session) return;
    try {
      const r: any = await services.notify("/v1/notify", {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setItems(r.items ?? []);
      setErr(null);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to load");
    }
  }, [session]);
  useEffect(() => { load(); }, [load]);

  const counts = useMemo<Record<Filter, number>>(() => {
    const c: Record<Filter, number> = {
      all: 0, sent: 0, pending: 0, failed: 0, suppressed: 0,
    };
    if (!items) return c;
    c.all = items.length;
    for (const it of items) {
      if (it.status in c) c[it.status as Filter] += 1;
    }
    return c;
  }, [items]);

  const visible = useMemo(() => {
    if (!items) return [];
    return filter === "all" ? items : items.filter((i) => i.status === filter);
  }, [items, filter]);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Notifications</h1>
        <p className="text-sm text-slate-600">
          Last 200 outbound messages across all channels and templates.
          Failed and suppressed rows are surfaced here so delivery
          problems don't sit silently in the database.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        {FILTERS.map((f) => (
          <button key={f} onClick={() => setFilter(f)}
            className={`px-3 py-1.5 rounded-full text-xs ring-1 transition ${
              filter === f
                ? "bg-slate-900 text-white ring-slate-900"
                : "bg-white text-slate-700 ring-slate-300 hover:bg-slate-50"
            }`}>
            {f} <span className="opacity-60">({counts[f]})</span>
          </button>
        ))}
        <button onClick={load}
          className="ml-auto px-3 py-1.5 rounded-full text-xs ring-1 ring-slate-300 bg-white hover:bg-slate-50">
          Refresh
        </button>
      </div>

      {err && <Card className="text-red-700">{err}</Card>}

      {items === null && <Card>Loading…</Card>}
      {items !== null && visible.length === 0 && (
        <Card>
          <p className="text-sm text-slate-500">
            No {filter === "all" ? "" : filter} notifications.
          </p>
        </Card>
      )}

      {visible.length > 0 && (
        <Card className="p-0 overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-600">
              <tr>
                <th className="text-left p-3">When</th>
                <th className="text-left p-3">Channel</th>
                <th className="text-left p-3">Template</th>
                <th className="text-left p-3">Recipient</th>
                <th className="text-left p-3">Status</th>
                <th className="text-left p-3">Provider</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((it) => (
                <tr key={it.id} className="border-t border-slate-100 align-top">
                  <td className="p-3 text-xs whitespace-nowrap">
                    {new Date(it.created_at).toLocaleString()}
                  </td>
                  <td className="p-3"><Pill>{it.channel}</Pill></td>
                  <td className="p-3 font-mono text-xs">
                    {it.template ?? <span className="text-slate-400">—</span>}
                  </td>
                  <td className="p-3 font-mono text-xs break-all">{it.recipient}</td>
                  <td className="p-3">
                    <Pill tone={TONE[it.status] ?? "slate"}>{it.status}</Pill>
                  </td>
                  <td className="p-3 text-xs">
                    {it.provider || <span className="text-slate-400">—</span>}
                    {it.provider_ref && (
                      <div className="font-mono text-slate-500 mt-0.5">
                        {it.provider_ref.slice(0, 24)}
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}
