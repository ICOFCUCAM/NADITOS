"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession, Button } from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string;
  issued_at: string; due_at: string;
};

export default function MyFinesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[]>([]);
  const [busy, setBusy] = useState<string | null>(null);

  async function refresh() {
    if (!session) return;
    const r: any = await services.fines("/v1/fines/mine", {
      token: session.accessToken, tenant: session.user.tenant,
    });
    setItems(r.items ?? []);
  }
  useEffect(() => { refresh(); }, [session]);

  async function pay(id: string) {
    if (!session) return;
    setBusy(id);
    try {
      await services.fines(`/v1/fines/${id}/pay`, {
        method: "POST", body: { method: "card" },
        token: session.accessToken, tenant: session.user.tenant,
      });
      await refresh();
    } finally { setBusy(null); }
  }

  return (
    <>
      <h1 className="text-2xl font-bold">My fines</h1>
      {items.length === 0 && <Card>No fines on your account.</Card>}
      {items.map((f) => (
        <Card key={f.id}>
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="font-mono">{f.plate}</div>
              <div className="text-sm text-slate-600">{f.offence_code}</div>
              <div className="text-sm">{f.amount} {f.currency}</div>
              <div className="text-xs text-slate-500">
                Issued {new Date(f.issued_at).toLocaleDateString()} · Due {new Date(f.due_at).toLocaleDateString()}
              </div>
            </div>
            <div className="flex flex-col items-end gap-2">
              <Pill tone={f.status === "paid" ? "green" : "amber"}>{f.status}</Pill>
              {f.status !== "paid" && f.status !== "cancelled" && (
                <Button onClick={() => pay(f.id)} disabled={busy === f.id}>
                  {busy === f.id ? "Paying…" : "Pay now"}
                </Button>
              )}
            </div>
          </div>
        </Card>
      ))}
    </>
  );
}
