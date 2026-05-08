"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Scan = {
  id: string; plate: string; confidence: number; source: string;
  captured_at: string; geo_lat: number | null; geo_lng: number | null;
  matched_vehicle_id: string | null;
};

export default function AnprFeedPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Scan[]>([]);

  useEffect(() => {
    if (!session) return;
    let alive = true;
    const tick = () =>
      services.anpr("/v1/anpr/scans", {
        token: session.accessToken, tenant: session.user.tenant,
      })
        .then((r: any) => alive && setItems(r.items ?? []))
        .catch(() => {});
    tick();
    const id = setInterval(tick, 4000);
    return () => { alive = false; clearInterval(id); };
  }, [session]);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">ANPR feed</h1>
        <p className="text-sm text-slate-600">
          Live stream of plate reads from officer devices, fixed cameras,
          tolls, border posts. Refreshing every 4 seconds.
        </p>
      </div>
      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">When</th>
              <th className="text-left p-3">Plate</th>
              <th className="text-left p-3">Source</th>
              <th className="text-left p-3">Confidence</th>
              <th className="text-left p-3">Matched</th>
            </tr>
          </thead>
          <tbody>
            {items.map((s) => (
              <tr key={s.id} className="border-t border-slate-100">
                <td className="p-3">{new Date(s.captured_at).toLocaleTimeString()}</td>
                <td className="p-3 font-mono">{s.plate}</td>
                <td className="p-3">{s.source}</td>
                <td className="p-3">{(s.confidence * 100).toFixed(0)}%</td>
                <td className="p-3">
                  {s.matched_vehicle_id
                    ? <Pill tone="green">match</Pill>
                    : <Pill>unknown</Pill>}
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={5}>No scans yet.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}
