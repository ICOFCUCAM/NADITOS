"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string;
  issued_at: string; due_at: string;
};

export default function FinesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[]>([]);

  useEffect(() => {
    if (!session) return;
    services.fines("/v1/fines", { token: session.accessToken, tenant: session.user.tenant })
      .then((r: any) => setItems(r.items ?? []))
      .catch(() => setItems([]));
  }, [session]);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Fines</h1>

      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Plate</th>
              <th className="text-left p-3">Offence</th>
              <th className="text-left p-3">Amount</th>
              <th className="text-left p-3">Status</th>
              <th className="text-left p-3">Issued</th>
              <th className="text-left p-3">Due</th>
            </tr>
          </thead>
          <tbody>
            {items.map((f) => (
              <tr key={f.id} className="border-t border-slate-100">
                <td className="p-3 font-mono">{f.plate}</td>
                <td className="p-3">{f.offence_code}</td>
                <td className="p-3">{f.amount} {f.currency}</td>
                <td className="p-3">{tone(f.status)}</td>
                <td className="p-3">{fmt(f.issued_at)}</td>
                <td className="p-3">{fmt(f.due_at)}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={6}>No fines yet.</td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

function tone(status: string) {
  switch (status) {
    case "paid":      return <Pill tone="green">paid</Pill>;
    case "cancelled": return <Pill>cancelled</Pill>;
    case "disputed":  return <Pill tone="amber">disputed</Pill>;
    case "escalated": return <Pill tone="red">escalated</Pill>;
    default:          return <Pill tone="amber">{status}</Pill>;
  }
}
function fmt(iso: string) { return new Date(iso).toLocaleString(); }
