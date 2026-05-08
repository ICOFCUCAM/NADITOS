"use client";

// Citizen transfers page.
//
// Two surfaces in one place:
//
//   1. Outgoing — transfers I started. The list comes back from
//      GET /v1/citizens/me/transfers. Codes are visible only on
//      pending rows; the server redacts them on terminal rows.
//
//   2. Incoming — accept-by-code form. The buyer types the code the
//      seller gave them; on success they own the vehicle.
//
// We don't query "incoming pending" explicitly: a buyer with a code
// just needs to type it in. The flow keeps the seller in control
// (they hand over the code through whatever channel they prefer)
// without us building a separate buyer-side inbox.

import { useCallback, useEffect, useState } from "react";
import { Button, Card, Input, Pill, services, useSession } from "@naditos/web-common";

type Transfer = {
  id: string;
  vehicle_id: string;
  plate: string;
  code: string;
  to_contact: string;
  status: "pending" | "accepted" | "cancelled" | "expired";
  created_at: string;
  expires_at: string;
  accepted_at?: string | null;
};

const STATUS_TONE: Record<Transfer["status"], "amber" | "green" | "slate" | "red"> = {
  pending:   "amber",
  accepted:  "green",
  cancelled: "slate",
  expired:   "red",
};

export default function TransfersPage() {
  const { session } = useSession();
  const [out, setOut] = useState<Transfer[] | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!session) return;
    const r: any = await services.registry("/v1/citizens/me/transfers", {
      token: session.accessToken, tenant: session.user.tenant,
    });
    setOut(r.items ?? []);
  }, [session]);

  useEffect(() => { refresh(); }, [refresh]);

  async function cancel(id: string) {
    if (!session) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      await services.registry(`/v1/citizens/me/transfers/${id}/cancel`, {
        method: "POST", token: session.accessToken, tenant: session.user.tenant,
      });
      await refresh();
    } catch (e: any) { setErr(e?.message ?? "Cancel failed"); }
    finally { setBusy(false); }
  }

  async function accept() {
    if (!session || !code.trim()) return;
    setBusy(true); setErr(null); setMsg(null);
    try {
      const r: any = await services.registry("/v1/citizens/me/transfers/accept", {
        method: "POST", body: { code: code.trim().toUpperCase() },
        token: session.accessToken, tenant: session.user.tenant,
      });
      setMsg("Transfer accepted. Vehicle " + r.vehicle_id.slice(0, 8) + "… is now yours.");
      setCode("");
      await refresh();
    } catch (e: any) { setErr(e?.message ?? "Accept failed"); }
    finally { setBusy(false); }
  }

  return (
    <>
      <h1 className="text-2xl font-bold">Ownership transfers</h1>

      <Card>
        <div className="space-y-2">
          <div className="font-semibold">Accept a transfer</div>
          <p className="text-sm text-slate-600">
            Enter the code the seller shared with you. You'll need a
            profile first — make sure you've completed{" "}
            <a href="/owner" className="underline">My profile</a>.
          </p>
          <div className="flex gap-2">
            <Input
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
              placeholder="ABC123"
              className="font-mono uppercase tracking-widest"
            />
            <Button onClick={accept} disabled={!code.trim() || busy}>
              {busy ? "Accepting…" : "Accept"}
            </Button>
          </div>
          {msg && <p className="text-sm text-emerald-700">{msg}</p>}
          {err && <p className="text-sm text-red-600">{err}</p>}
        </div>
      </Card>

      <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-4">
        Transfers I've started
      </h2>
      {out === null && <Card>Loading…</Card>}
      {out !== null && out.length === 0 && (
        <Card>No transfers started yet.</Card>
      )}
      {out?.map((t) => (
        <Card key={t.id}>
          <div className="flex items-start justify-between gap-3">
            <div className="space-y-1">
              <div className="font-mono text-lg">{t.plate}</div>
              <div className="text-xs text-slate-500">
                To: {t.to_contact}
              </div>
              {t.code && (
                <div className="font-mono text-xl tracking-widest pt-1">
                  {t.code}
                </div>
              )}
              <div className="text-xs text-slate-500 pt-1">
                Started {new Date(t.created_at).toLocaleDateString()} ·
                {t.status === "pending"
                  ? <> expires {new Date(t.expires_at).toLocaleDateString()}</>
                  : t.accepted_at
                    ? <> accepted {new Date(t.accepted_at).toLocaleDateString()}</>
                    : null}
              </div>
            </div>
            <div className="flex flex-col items-end gap-2">
              <Pill tone={STATUS_TONE[t.status]}>{t.status}</Pill>
              {t.status === "pending" && (
                <button onClick={() => cancel(t.id)} disabled={busy}
                  className="text-xs text-slate-600 hover:underline">
                  Cancel
                </button>
              )}
            </div>
          </div>
        </Card>
      ))}
    </>
  );
}
