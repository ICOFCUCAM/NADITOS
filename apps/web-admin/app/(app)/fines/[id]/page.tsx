"use client";

// Admin fine detail.
//
// Renders the same fine + evidence + chain-of-custody view the citizen
// sees, plus admin-only actions: cancel (with reason). Escalation stage
// is surfaced loud because admins use this page during enforcement
// triage.

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { Button, Card, Pill, services, useSession } from "@naditos/web-common";

type Evidence = {
  kind: string;
  s3_key: string;
  sha256: string;
  bytes: number;
  taken_at: string;
};

type Custody = {
  evidence_id: string;
  action: string;
  actor_user?: string | null;
  actor_role?: string | null;
  actor_device?: string | null;
  details?: any;
  occurred_at: string;
};

type Fine = {
  id: string;
  plate: string;
  offence_code: string;
  amount: string;
  currency: string;
  status: string;
  issued_at: string;
  due_at: string;
  issued_by: string;
  escalation_stage: number;
  evidence?: Evidence[];
  custody?: Custody[];
};

const STAGE_LABEL: Record<number, string> = {
  1: "Stage 1 · warning",
  2: "Stage 2 · penalty",
  3: "Stage 3 · flag",
  4: "Stage 4 · seize",
  5: "Stage 5 · court",
};

export default function FineDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { session } = useSession();
  const [fine, setFine] = useState<Fine | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!session || !id) return;
    try {
      const r = await services.fines(`/v1/fines/${id}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setFine(r as Fine);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to load");
    }
  }, [session, id]);
  useEffect(() => { load(); }, [load]);

  async function cancelFine() {
    if (!session || !fine) return;
    const reason = window.prompt(
      "Cancellation reason (will be recorded in the audit log):");
    if (!reason || !reason.trim()) return;
    setBusy(true);
    try {
      await services.fines(`/v1/fines/${fine.id}/cancel`, {
        method: "POST", body: { reason: reason.trim() },
        token: session.accessToken, tenant: session.user.tenant,
      });
      await load();
    } catch (e: any) {
      setErr(e?.message ?? "Cancel failed");
    } finally {
      setBusy(false);
    }
  }

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (!fine) return <Card>Loading…</Card>;

  const isOpen = ["issued", "warned", "overdue", "escalated"].includes(fine.status);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Fine {fine.id.slice(0, 8)}…</h1>
        <Link href="/fines" className="text-sm text-slate-600 hover:underline">
          ← All fines
        </Link>
      </div>

      <Card>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>Plate: <span className="font-mono">{fine.plate}</span></div>
          <div>Offence: <span className="font-mono">{fine.offence_code}</span></div>
          <div>Amount: <span className="font-medium">{fine.amount} {fine.currency}</span></div>
          <div>Status: <Pill tone={fine.status === "paid" ? "green" :
                                   fine.status === "cancelled" || fine.status === "disputed" ? "slate" :
                                   "amber"}>{fine.status}</Pill></div>
          <div>Issued: {new Date(fine.issued_at).toLocaleString()}</div>
          <div>Due: {new Date(fine.due_at).toLocaleString()}</div>
          <div className="col-span-2 text-xs text-slate-500">
            Officer: <span className="font-mono">{fine.issued_by}</span>
          </div>
          {fine.escalation_stage > 0 && (
            <div className="col-span-2">
              <Pill tone="red">{STAGE_LABEL[fine.escalation_stage] ?? `Stage ${fine.escalation_stage}`}</Pill>
            </div>
          )}
        </div>
        {isOpen && (
          <div className="mt-4 pt-3 border-t border-slate-100">
            <Button onClick={cancelFine} disabled={busy}
              className="bg-red-600 hover:bg-red-700">
              {busy ? "Cancelling…" : "Cancel fine"}
            </Button>
          </div>
        )}
      </Card>

      <Card className="p-0 overflow-hidden">
        <div className="bg-slate-50 px-4 py-2 text-sm font-medium">
          Evidence ({(fine.evidence ?? []).length})
        </div>
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Kind</th>
              <th className="text-left p-3">Captured</th>
              <th className="text-left p-3">Bytes</th>
              <th className="text-left p-3">SHA-256</th>
            </tr>
          </thead>
          <tbody>
            {(fine.evidence ?? []).map((e, i) => (
              <tr key={i} className="border-t border-slate-100">
                <td className="p-3"><Pill>{e.kind}</Pill></td>
                <td className="p-3">{new Date(e.taken_at).toLocaleString()}</td>
                <td className="p-3">{e.bytes.toLocaleString()}</td>
                <td className="p-3 font-mono text-xs break-all">{e.sha256.slice(0, 32)}…</td>
              </tr>
            ))}
            {(fine.evidence ?? []).length === 0 && (
              <tr><td className="p-6 text-center text-red-600" colSpan={4}>
                No evidence — this should be impossible (anti-corruption gate).
              </td></tr>
            )}
          </tbody>
        </table>
      </Card>

      <Card className="p-0 overflow-hidden">
        <div className="bg-slate-50 px-4 py-2 text-sm font-medium">
          Chain of custody ({(fine.custody ?? []).length})
        </div>
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">When</th>
              <th className="text-left p-3">Action</th>
              <th className="text-left p-3">Actor</th>
              <th className="text-left p-3">Device</th>
            </tr>
          </thead>
          <tbody>
            {(fine.custody ?? []).map((c, i) => (
              <tr key={i} className="border-t border-slate-100 align-top">
                <td className="p-3 text-xs">{new Date(c.occurred_at).toLocaleString()}</td>
                <td className="p-3"><Pill>{c.action}</Pill></td>
                <td className="p-3 font-mono text-xs">
                  {c.actor_role ?? "—"}
                  {c.actor_user && <> · {c.actor_user.slice(0, 8)}</>}
                </td>
                <td className="p-3 font-mono text-xs">{c.actor_device ?? "—"}</td>
              </tr>
            ))}
            {(fine.custody ?? []).length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={4}>
                No custody events recorded.
              </td></tr>
            )}
          </tbody>
        </table>
      </Card>

      <p className="text-xs text-slate-500">
        Object storage URLs (s3_key) are intentionally not rendered as preview
        images here; the secure evidence-viewer endpoint signs short-lived
        URLs after a chain-of-custody check. Phase-3 wires the preview iframe.
      </p>
    </div>
  );
}
