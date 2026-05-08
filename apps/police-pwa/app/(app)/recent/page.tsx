"use client";

import { useEffect, useState } from "react";
import { services, useSession, Pill } from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string; issued_at: string;
};

export default function RecentPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[]>([]);
  useEffect(() => {
    if (!session) return;
    services.fines("/v1/fines", { token: session.accessToken, tenant: session.user.tenant })
      .then((r: any) => setItems(r.items ?? [])).catch(() => setItems([]));
  }, [session]);
  return (
    <div className="p-4 space-y-3">
      <h1 className="text-xl font-bold">Recent fines</h1>
      {items.length === 0 && <p className="text-slate-400 text-sm">No fines issued yet.</p>}
      {items.map((f) => (
        <div key={f.id} className="rounded-lg bg-slate-800 p-3">
          <div className="flex justify-between">
            <div className="font-mono">{f.plate}</div>
            <Pill tone={f.status === "paid" ? "green" : "amber"}>{f.status}</Pill>
          </div>
          <div className="text-sm text-slate-300">{f.offence_code}</div>
          <div className="text-sm">{f.amount} {f.currency}</div>
          <div className="text-xs text-slate-400">{new Date(f.issued_at).toLocaleString()}</div>
        </div>
      ))}
    </div>
  );
}
