"use client";

import { useEffect, useState } from "react";
import { Card, Input, Pill, services, useSession, statusBadgeClasses, statusLabel, type VehicleStatus } from "@naditos/web-common";

type Vehicle = {
  id: string; plate: string; make?: string; model?: string; year?: number;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
};

export default function VehiclesPage() {
  const { session } = useSession();
  const [q, setQ] = useState("");
  const [items, setItems] = useState<Vehicle[]>([]);

  useEffect(() => {
    if (!session) return;
    const t = setTimeout(() => {
      services.registry(`/v1/vehicles?q=${encodeURIComponent(q)}`, {
        token: session.accessToken, tenant: session.user.tenant,
      }).then((r: any) => setItems(r.items ?? [])).catch(() => setItems([]));
    }, 200);
    return () => clearTimeout(t);
  }, [q, session]);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Vehicle registry</h1>
      <Input placeholder="Search plate or VIN…" value={q} onChange={(e) => setQ(e.target.value)} />

      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Plate</th>
              <th className="text-left p-3">Make/Model</th>
              <th className="text-left p-3">Status</th>
              <th className="text-left p-3">Insurance</th>
              <th className="text-left p-3">Inspection</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id} className="border-t border-slate-100">
                <td className="p-3 font-mono">{v.plate}</td>
                <td className="p-3">{[v.make, v.model, v.year].filter(Boolean).join(" ")}</td>
                <td className="p-3">
                  <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs ring-1 ${statusBadgeClasses(v.status)}`}>
                    {statusLabel[v.status]}
                  </span>
                  {v.is_stolen && <Pill tone="black">stolen</Pill>}
                </td>
                <td className="p-3">{fmt(v.insurance_expires_at)}</td>
                <td className="p-3">{fmt(v.inspection_expires_at)}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={5}>No vehicles match.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

function fmt(iso?: string | null) {
  if (!iso) return "—";
  return new Date(iso).toISOString().slice(0, 10);
}
