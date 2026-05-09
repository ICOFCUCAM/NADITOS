"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Card, Input, Pill, services, useSession, statusBadgeClasses, statusLabel, type VehicleStatus } from "@naditos/web-common";

type Vehicle = {
  id: string; plate: string; make?: string; model?: string; year?: number;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
  is_seized: boolean;
  is_wanted: boolean;
};

export default function VehiclesPage() {
  const { session } = useSession();
  const [q, setQ] = useState("");
  const [flaggedOnly, setFlaggedOnly] = useState(false);
  const [items, setItems] = useState<Vehicle[]>([]);

  useEffect(() => {
    if (!session) return;
    const t = setTimeout(() => {
      const params = new URLSearchParams();
      if (q) params.set("q", q);
      if (flaggedOnly) params.set("flagged", "1");
      services.registry(`/v1/vehicles?${params.toString()}`, {
        token: session.accessToken, tenant: session.user.tenant,
      }).then((r: any) => setItems(r.items ?? [])).catch(() => setItems([]));
    }, 200);
    return () => clearTimeout(t);
  }, [q, flaggedOnly, session]);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Vehicle registry</h1>
      <div className="flex flex-wrap items-center gap-3">
        <Input placeholder="Search plate or VIN…" value={q}
          onChange={(e) => setQ(e.target.value)}
          className="flex-1 min-w-[14rem]" />
        <label className="text-sm flex items-center gap-2 select-none">
          <input type="checkbox" checked={flaggedOnly}
            onChange={(e) => setFlaggedOnly(e.target.checked)} />
          Flagged only (stolen / seized / wanted)
        </label>
      </div>

      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Plate</th>
              <th className="text-left p-3">Make/Model</th>
              <th className="text-left p-3">Status</th>
              <th className="text-left p-3">Flags</th>
              <th className="text-left p-3">Insurance</th>
              <th className="text-left p-3">Inspection</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id} className="border-t border-slate-100 hover:bg-slate-50">
                <td className="p-3 font-mono">
                  <Link href={`/vehicles/${v.id}`} className="hover:underline">{v.plate}</Link>
                </td>
                <td className="p-3">{[v.make, v.model, v.year].filter(Boolean).join(" ")}</td>
                <td className="p-3">
                  <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs ring-1 ${statusBadgeClasses(v.status)}`}>
                    {statusLabel[v.status]}
                  </span>
                </td>
                <td className="p-3 space-x-1">
                  {v.is_stolen && <Pill tone="black">stolen</Pill>}
                  {v.is_seized && <Pill tone="red">seized</Pill>}
                  {v.is_wanted && <Pill tone="amber">wanted</Pill>}
                  {!v.is_stolen && !v.is_seized && !v.is_wanted && (
                    <span className="text-slate-300">—</span>
                  )}
                </td>
                <td className="p-3">{fmt(v.insurance_expires_at)}</td>
                <td className="p-3">{fmt(v.inspection_expires_at)}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={6}>
                {flaggedOnly ? "No flagged vehicles." : "No vehicles match."}
              </td></tr>
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
