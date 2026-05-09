"use client";

// Citizen fines history.
//
// Server pre-conditions this page relies on:
//   - GET  /v1/fines/mine returns fines owned via the linked citizen
//     (vehicle owner OR fines.driver_user_id) — RLS keeps tenant scope
//   - POST /v1/fines/{id}/pay     — synchronous on dev-stub, returns paid
//   - POST /v1/fines/{id}/dispute — accepts {reason}, gated by owner
//                                  and only allowed on open statuses
//
// UI choices:
//   - Open vs Closed sections so a citizen with a long history doesn't
//     have to scroll past paid items to see what's still owed
//   - Overdue badge when status is overdue OR due_at is in the past on
//     a still-open status — the server might not have escalated yet
//   - Escalation stage label so citizens see how serious it is
//   - Dispute opens a small reason form inline; pay is one-click

import Link from "next/link";
import { useEffect, useState } from "react";
import { Card, Pill, services, useSession, Button } from "@naditos/web-common";

type Fine = {
  id: string;
  plate: string;
  offence_code: string;
  amount: string;
  currency: string;
  status: string;
  issued_at: string;
  due_at: string;
  escalation_stage: number;
};

const STAGE_LABEL: Record<number, string> = {
  1: "Stage 1 · warning",
  2: "Stage 2 · penalty",
  3: "Stage 3 · flag",
  4: "Stage 4 · seize",
  5: "Stage 5 · court",
};

const OPEN_STATUSES = new Set(["issued", "warned", "overdue", "escalated"]);
const CLOSED_STATUSES = new Set(["paid", "cancelled", "disputed", "court", "seized"]);

function statusTone(status: string, overdue: boolean): "green" | "amber" | "red" | "slate" {
  if (status === "paid") return "green";
  if (status === "cancelled" || status === "disputed") return "slate";
  if (status === "court" || status === "seized") return "red";
  if (overdue) return "red";
  return "amber";
}

export default function MyFinesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [disputing, setDisputing] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [err, setErr] = useState<string | null>(null);

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
    setBusy(id); setErr(null);
    try {
      await services.fines(`/v1/fines/${id}/pay`, {
        method: "POST", body: { method: "card" },
        token: session.accessToken, tenant: session.user.tenant,
      });
      await refresh();
    } catch (e: any) { setErr(e?.message ?? "Payment failed"); }
    finally { setBusy(null); }
  }

  async function submitDispute(id: string) {
    if (!session || !reason.trim()) return;
    setBusy(id); setErr(null);
    try {
      await services.fines(`/v1/fines/${id}/dispute`, {
        method: "POST", body: { reason: reason.trim() },
        token: session.accessToken, tenant: session.user.tenant,
      });
      setDisputing(null); setReason("");
      await refresh();
    } catch (e: any) { setErr(e?.message ?? "Dispute failed"); }
    finally { setBusy(null); }
  }

  const open = items.filter((f) => OPEN_STATUSES.has(f.status));
  const closed = items.filter((f) => CLOSED_STATUSES.has(f.status));

  function row(f: Fine) {
    const overdue = OPEN_STATUSES.has(f.status) && new Date(f.due_at).getTime() < Date.now();
    const canPay     = OPEN_STATUSES.has(f.status);
    const canDispute = OPEN_STATUSES.has(f.status);
    return (
      <Card key={f.id}>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Link href={`/fines/${f.id}`} className="font-mono text-lg hover:underline">
              {f.plate}
            </Link>
            <div className="text-sm text-slate-600">{f.offence_code}</div>
            <div className="text-base font-semibold">{f.amount} {f.currency}</div>
            <div className="text-xs text-slate-500">
              Issued {new Date(f.issued_at).toLocaleDateString()} ·
              {" "}Due {new Date(f.due_at).toLocaleDateString()}
            </div>
            {f.escalation_stage > 0 && (
              <div className="text-xs text-amber-700">
                {STAGE_LABEL[f.escalation_stage] ?? `Stage ${f.escalation_stage}`}
              </div>
            )}
          </div>
          <div className="flex flex-col items-end gap-2 shrink-0">
            <div className="flex gap-1">
              {overdue && <Pill tone="red">overdue</Pill>}
              <Pill tone={statusTone(f.status, overdue)}>{f.status}</Pill>
            </div>
            {canPay && (
              <Button onClick={() => pay(f.id)} disabled={busy === f.id}>
                {busy === f.id ? "Paying…" : "Pay now"}
              </Button>
            )}
            {canDispute && disputing !== f.id && (
              <button
                onClick={() => { setDisputing(f.id); setReason(""); }}
                className="text-xs text-slate-600 hover:underline">
                Dispute
              </button>
            )}
          </div>
        </div>
        {disputing === f.id && (
          <div className="mt-3 pt-3 border-t border-slate-100 space-y-2">
            <label className="block text-xs uppercase tracking-wide text-slate-500">
              Reason for dispute
            </label>
            <textarea
              value={reason} onChange={(e) => setReason(e.target.value)}
              rows={3}
              className="w-full rounded border border-slate-300 p-2 text-sm"
              placeholder="Describe what happened. Officers and reviewers will read this."
            />
            <div className="flex gap-2 justify-end">
              <button
                onClick={() => { setDisputing(null); setReason(""); }}
                className="text-sm text-slate-600 hover:underline">
                Cancel
              </button>
              <Button
                onClick={() => submitDispute(f.id)}
                disabled={!reason.trim() || busy === f.id}>
                {busy === f.id ? "Submitting…" : "Submit dispute"}
              </Button>
            </div>
          </div>
        )}
      </Card>
    );
  }

  return (
    <>
      <h1 className="text-2xl font-bold">My fines</h1>
      {err && <Card><span className="text-red-600 text-sm">{err}</span></Card>}
      {items.length === 0 && <Card>No fines on your account.</Card>}

      {open.length > 0 && (
        <>
          <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-2">Open</h2>
          {open.map(row)}
        </>
      )}
      {closed.length > 0 && (
        <>
          <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-4">Closed</h2>
          {closed.map(row)}
        </>
      )}
    </>
  );
}
