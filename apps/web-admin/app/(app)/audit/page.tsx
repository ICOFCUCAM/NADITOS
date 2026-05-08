"use client";

import { useCallback, useEffect, useState } from "react";
import { Card, services, useSession, Pill } from "@naditos/web-common";

type Event = {
  id: number;
  occurred_at: string;
  actor_user?: string | null;
  actor_role?: string | null;
  service: string;
  action: string;
  resource_type: string;
  resource_id?: string | null;
  hash: string;
};

type Alert = {
  id: number;
  kind: string;
  subject_kind?: string | null;
  subject_id?: string | null;
  day: string;
  severity?: number | null;
  details: Record<string, unknown>;
  detected_at: string;
};

const ALERT_LABELS: Record<string, string> = {
  officer_high_anomaly_z:   "Anomalous fine volume",
  officer_high_cancel_rate: "High cancellation rate",
};

export default function AuditPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Event[]>([]);
  const [verify, setVerify] = useState<{ ok: boolean; checked: number } | null>(null);
  const [alerts, setAlerts] = useState<Alert[]>([]);

  const loadAlerts = useCallback(async () => {
    if (!session) return;
    const r = await services.audit(`/v1/audit/alerts`, {
      token: session.accessToken, tenant: session.user.tenant,
    });
    setAlerts((r as any).items ?? []);
  }, [session]);

  useEffect(() => {
    if (!session) return;
    services.audit(`/v1/audit/events?tenant_id=${session.user.tenant}`, {
      token: session.accessToken, tenant: session.user.tenant,
    }).then((r: any) => setItems(r.items ?? []));
    loadAlerts();
  }, [session, loadAlerts]);

  async function runVerify() {
    if (!session) return;
    const r = await services.audit(`/v1/audit/verify?tenant_id=${session.user.tenant}`, {
      token: session.accessToken, tenant: session.user.tenant,
    });
    setVerify(r as any);
  }

  async function resolve(alertId: number) {
    if (!session) return;
    const note = window.prompt("Resolution note (optional):") ?? "";
    await services.audit(`/v1/audit/alerts/${alertId}/resolve`, {
      method: "POST", token: session.accessToken, tenant: session.user.tenant,
      body: { resolution: note },
    });
    loadAlerts();
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Audit log</h1>
        <button onClick={runVerify}
          className="text-sm rounded bg-slate-900 text-white px-3 py-1.5 hover:bg-slate-800">
          Verify chain
        </button>
      </div>
      {verify && (
        <Card>
          {verify.ok
            ? <Pill tone="green">Chain valid · {verify.checked} events</Pill>
            : <Pill tone="red">Chain broken at event #{(verify as any).broken_at}</Pill>}
        </Card>
      )}
      {alerts.length > 0 && (
        <Card className="p-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-slate-100 flex items-center justify-between">
            <div className="font-semibold">Open anomaly alerts</div>
            <Pill tone="red">{alerts.length}</Pill>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-600">
              <tr>
                <th className="text-left p-3">Detected</th>
                <th className="text-left p-3">Day</th>
                <th className="text-left p-3">Kind</th>
                <th className="text-left p-3">Subject</th>
                <th className="text-left p-3">Severity</th>
                <th className="text-left p-3">Details</th>
                <th className="p-3"></th>
              </tr>
            </thead>
            <tbody>
              {alerts.map((a) => (
                <tr key={a.id} className="border-t border-slate-100 align-top">
                  <td className="p-3 text-xs">{new Date(a.detected_at).toLocaleString()}</td>
                  <td className="p-3">{new Date(a.day).toLocaleDateString()}</td>
                  <td className="p-3">{ALERT_LABELS[a.kind] ?? a.kind}</td>
                  <td className="p-3 font-mono text-xs">
                    {a.subject_kind ?? "—"}{a.subject_id ? ` · ${a.subject_id.slice(0, 8)}` : ""}
                  </td>
                  <td className="p-3">
                    {a.severity != null ? a.severity.toFixed(2) : "—"}
                  </td>
                  <td className="p-3 font-mono text-xs">{JSON.stringify(a.details)}</td>
                  <td className="p-3 text-right">
                    <button onClick={() => resolve(a.id)}
                      className="text-xs rounded bg-slate-900 text-white px-2 py-1 hover:bg-slate-800">
                      Resolve
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">When</th>
              <th className="text-left p-3">Actor</th>
              <th className="text-left p-3">Service</th>
              <th className="text-left p-3">Action</th>
              <th className="text-left p-3">Resource</th>
              <th className="text-left p-3">Hash</th>
            </tr>
          </thead>
          <tbody>
            {items.map((e) => (
              <tr key={e.id} className="border-t border-slate-100">
                <td className="p-3">{new Date(e.occurred_at).toLocaleString()}</td>
                <td className="p-3 font-mono text-xs">
                  {e.actor_role ?? "—"}{e.actor_user ? ` · ${e.actor_user.slice(0,8)}` : ""}
                </td>
                <td className="p-3">{e.service}</td>
                <td className="p-3">{e.action}</td>
                <td className="p-3">{e.resource_type} {e.resource_id ? `· ${e.resource_id.slice(0,8)}` : ""}</td>
                <td className="p-3 font-mono text-xs">{e.hash.slice(0, 16)}…</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={6}>No events.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}
