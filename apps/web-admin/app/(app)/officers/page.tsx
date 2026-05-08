"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession, Button } from "@naditos/web-common";

type Stat = {
  officer_id: string;
  officer_name: string;
  day: string;
  fines_issued: number;
  fines_cancelled: number;
  fines_total: string;
  unique_plates: number;
  anomaly_score: number | null;
};

// Anomaly bands from the within-officer z-score:
//
//   |z| < 1.5   green    — within normal range
//   |z| 1.5..3  amber    — worth a glance
//   |z| ≥ 3     red      — outlier; admin should review
//   null        slate    — insufficient baseline (<3 prior days)
function bandFor(z: number | null): { tone: "green" | "amber" | "red" | "slate"; label: string } {
  if (z === null) return { tone: "slate", label: "no baseline" };
  const abs = Math.abs(z);
  if (abs >= 3) return { tone: "red", label: `${z >= 0 ? "+" : ""}${z.toFixed(1)}σ` };
  if (abs >= 1.5) return { tone: "amber", label: `${z >= 0 ? "+" : ""}${z.toFixed(1)}σ` };
  return { tone: "green", label: `${z >= 0 ? "+" : ""}${z.toFixed(1)}σ` };
}

export default function OfficersPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Stat[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function refresh() {
    if (!session) return;
    try {
      const r: any = await services.audit("/v1/audit/officers/stats", {
        token: session.accessToken,
        tenant: session.user.tenant,
      });
      setItems(r.items ?? []);
      setErr(null);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to load");
    }
  }

  useEffect(() => {
    refresh();
  }, [session]);

  async function rebuild() {
    if (!session) return;
    setBusy(true);
    try {
      await services.audit("/v1/audit/officers/stats:rebuild", {
        method: "POST",
        token: session.accessToken,
        tenant: session.user.tenant,
      });
      await refresh();
    } catch (e: any) {
      setErr(e?.message ?? "Rebuild failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold">Officer activity</h1>
          <p className="text-sm text-slate-600">
            Daily fines per officer with a within-officer z-score against
            their own 14-day rolling baseline. Outliers (|z| ≥ 3σ) are
            anti-corruption signals worth reviewing.
          </p>
        </div>
        <Button onClick={rebuild} disabled={busy}>
          {busy ? "Rebuilding…" : "Recompute"}
        </Button>
      </div>

      {err && <Card className="text-red-700">{err}</Card>}

      {items === null ? (
        <Card>Loading…</Card>
      ) : items.length === 0 ? (
        <Card>
          <div className="text-sm text-slate-600">
            No officer activity in the last 14 days. The aggregator runs
            every hour automatically; click <em>Recompute</em> to refresh.
          </div>
        </Card>
      ) : (
        <Card className="p-0 overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-600">
              <tr>
                <th className="text-left p-3">Day</th>
                <th className="text-left p-3">Officer</th>
                <th className="text-right p-3">Issued</th>
                <th className="text-right p-3">Cancelled</th>
                <th className="text-right p-3">Total</th>
                <th className="text-right p-3">Unique plates</th>
                <th className="text-left p-3">Score</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s, i) => {
                const b = bandFor(s.anomaly_score);
                return (
                  <tr key={`${s.officer_id}-${s.day}-${i}`} className="border-t border-slate-100">
                    <td className="p-3 font-mono text-xs">
                      {new Date(s.day).toISOString().slice(0, 10)}
                    </td>
                    <td className="p-3">
                      <div>{s.officer_name || "—"}</div>
                      <div className="text-xs text-slate-500 font-mono">
                        {s.officer_id.slice(0, 8)}
                      </div>
                    </td>
                    <td className="p-3 text-right">{s.fines_issued}</td>
                    <td className="p-3 text-right">{s.fines_cancelled}</td>
                    <td className="p-3 text-right">{s.fines_total} EUR</td>
                    <td className="p-3 text-right">{s.unique_plates}</td>
                    <td className="p-3">
                      <Pill tone={b.tone}>{b.label}</Pill>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      )}

      <p className="text-xs text-slate-500">
        Score is the z-score of <code>fines_issued</code> against the
        officer&apos;s own previous 14 active days. A high positive score
        means many more fines than usual; high negative means many fewer.
        Insufficient-baseline rows (fewer than 3 prior days, or zero
        variance) are intentionally left unscored.
      </p>
    </div>
  );
}
