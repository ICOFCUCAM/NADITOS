"use client";

import { useEffect, useState } from "react";
import { Card, Pill, useSession, services } from "@naditos/web-common";

type Counts = {
  vehicles_total: number;
  vehicles_red: number;
  vehicles_black: number;
  fines_unpaid: number;
  fines_today: number;
};

export default function Dashboard() {
  const { session } = useSession();
  const [counts, setCounts] = useState<Counts | null>(null);

  useEffect(() => {
    // Phase-2: a real /v1/analytics/summary endpoint. For now, derive
    // the counts from a quick parallel pull.
    if (!session) return;
    Promise.all([
      services.registry("/v1/vehicles", { token: session.accessToken, tenant: session.user.tenant }),
      services.fines("/v1/fines", { token: session.accessToken, tenant: session.user.tenant }),
    ]).then(([v, f]: any) => {
      const items = v?.items ?? [];
      const fines = f?.items ?? [];
      const today = new Date().toISOString().slice(0, 10);
      setCounts({
        vehicles_total: items.length,
        vehicles_red:   items.filter((x: any) => x.status === "red").length,
        vehicles_black: items.filter((x: any) => x.status === "black").length,
        fines_unpaid:   fines.filter((x: any) => x.status !== "paid" && x.status !== "cancelled").length,
        fines_today:    fines.filter((x: any) => x.issued_at?.startsWith(today)).length,
      });
    }).catch(() => setCounts(null));
  }, [session]);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Ministry dashboard</h1>
        <p className="text-sm text-slate-600">
          Live operational status across the National Vehicle Intelligence Platform.
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Stat label="Vehicles registered" value={counts?.vehicles_total ?? "—"} />
        <Stat label="Non-compliant (red)"  value={counts?.vehicles_red   ?? "—"} tone="red" />
        <Stat label="Stolen / seized (black)" value={counts?.vehicles_black ?? "—"} tone="black" />
        <Stat label="Outstanding fines"   value={counts?.fines_unpaid   ?? "—"} tone="amber" />
        <Stat label="Fines issued today"  value={counts?.fines_today    ?? "—"} />
        <Card>
          <div className="text-sm text-slate-600">Modules enabled</div>
          <div className="mt-2 flex flex-wrap gap-2">
            <Pill tone="green">Registry</Pill>
            <Pill tone="green">Fines</Pill>
            <Pill tone="green">Audit</Pill>
            <Pill>Insurance (Phase-2)</Pill>
            <Pill>Inspection (Phase-2)</Pill>
            <Pill>ANPR (Phase-2)</Pill>
          </div>
        </Card>
      </div>
    </div>
  );
}

function Stat({ label, value, tone }:
  { label: string; value: number | string; tone?: "red"|"amber"|"black" }) {
  const cls = tone === "red"   ? "text-red-700"
            : tone === "amber" ? "text-amber-700"
            : tone === "black" ? "text-slate-900"
            : "text-slate-900";
  return (
    <Card>
      <div className="text-sm text-slate-600">{label}</div>
      <div className={`mt-2 text-3xl font-bold ${cls}`}>{value}</div>
    </Card>
  );
}
