"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  Button, Card, Input, Pill, services, useSession,
  statusBadgeClasses, statusLabel, type VehicleStatus,
} from "@naditos/web-common";

type Vehicle = {
  id: string;
  plate: string;
  make?: string | null;
  model?: string | null;
  year?: number | null;
  status: VehicleStatus;
  registration_expires_at?: string | null;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  tax_paid_through?: string | null;
  is_stolen: boolean;
  is_seized: boolean;
};

export default function MyVehiclesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Vehicle[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  // Per-vehicle transient transfer state. transferring[id] holds the
  // user's draft to_contact; issuedCodes[id] holds the {code, expires}
  // tuple after a successful start so the seller can read it back.
  const [transferring, setTransferring] = useState<Record<string, string>>({});
  const [issuedCodes, setIssuedCodes] = useState<Record<string, { code: string; expires_at: string }>>({});
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    if (!session) return;
    services.registry("/v1/citizens/me/vehicles", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setItems(r.items ?? []))
      .catch((e) => setErr(e?.message ?? "Failed to load"));
  }, [session]);

  async function startTransfer(vid: string) {
    if (!session) return;
    const to = (transferring[vid] ?? "").trim();
    if (!to) return;
    setBusy(vid); setErr(null);
    try {
      const r: any = await services.registry(
        `/v1/citizens/me/vehicles/${vid}/transfer`,
        {
          method: "POST", body: { to_contact: to },
          token: session.accessToken, tenant: session.user.tenant,
        });
      setIssuedCodes((m) => ({ ...m, [vid]: { code: r.code, expires_at: r.expires_at } }));
      setTransferring((m) => { const n = { ...m }; delete n[vid]; return n; });
    } catch (e: any) {
      setErr(e?.message ?? "Transfer failed");
    } finally { setBusy(null); }
  }

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (items === null) return <Card>Loading…</Card>;
  if (items.length === 0) {
    return (
      <>
        <h1 className="text-2xl font-bold">My vehicles</h1>
        <Card>
          <p className="text-sm text-slate-600">
            No vehicles are linked to your account yet. After your local
            transport ministry registers a vehicle to you they'll appear
            here automatically.
          </p>
          <p className="mt-3 text-sm text-slate-600">
            Make sure your{" "}
            <Link href="/owner" className="underline text-slate-900">
              profile
            </Link>{" "}
            is up to date so the registry can match you correctly.
          </p>
        </Card>
      </>
    );
  }

  return (
    <>
      <h1 className="text-2xl font-bold">My vehicles</h1>
      <div className="space-y-3">
        {items.map((v) => (
          <Card key={v.id}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="font-mono text-lg">{v.plate}</div>
                <div className="text-sm text-slate-600">
                  {[v.make, v.model, v.year].filter(Boolean).join(" ") || "—"}
                </div>
              </div>
              <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs ring-1 ${statusBadgeClasses(v.status)}`}>
                {statusLabel[v.status]}
              </span>
            </div>
            <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-slate-500">
              <div>Registration: {expiryBadge(v.registration_expires_at)}</div>
              <div>Insurance: {expiryBadge(v.insurance_expires_at)}</div>
              <div>Inspection: {expiryBadge(v.inspection_expires_at)}</div>
              <div>Road tax paid through: {expiryBadge(v.tax_paid_through)}</div>
            </div>
            {(v.is_stolen || v.is_seized) && (
              <div className="mt-2 flex flex-wrap gap-1">
                {v.is_stolen && <Pill tone="black">reported stolen</Pill>}
                {v.is_seized && <Pill tone="red">seized</Pill>}
              </div>
            )}

            {/* Transfer surface. issuedCodes[v.id] takes precedence so a
                seller who just generated a code sees it instead of the
                form they used to generate it. */}
            <div className="mt-4 pt-3 border-t border-slate-100">
              {issuedCodes[v.id] ? (
                <div className="space-y-1">
                  <div className="text-xs uppercase tracking-wide text-slate-500">
                    Transfer code
                  </div>
                  <div className="font-mono text-2xl tracking-widest">
                    {issuedCodes[v.id].code}
                  </div>
                  <div className="text-xs text-slate-500">
                    Share with the buyer. Expires{" "}
                    {new Date(issuedCodes[v.id].expires_at).toLocaleDateString()}.{" "}
                    <Link href="/transfers" className="underline">
                      Manage transfers
                    </Link>
                  </div>
                </div>
              ) : transferring[v.id] !== undefined ? (
                <div className="space-y-2">
                  <label className="block text-xs uppercase tracking-wide text-slate-500">
                    Buyer's email or phone
                  </label>
                  <Input
                    value={transferring[v.id]}
                    onChange={(e) => setTransferring((m) => ({ ...m, [v.id]: e.target.value }))}
                    placeholder="buyer@example.com"
                  />
                  <div className="flex gap-2 justify-end">
                    <button
                      onClick={() => setTransferring((m) => { const n = { ...m }; delete n[v.id]; return n; })}
                      className="text-sm text-slate-600 hover:underline">
                      Cancel
                    </button>
                    <Button
                      onClick={() => startTransfer(v.id)}
                      disabled={!transferring[v.id]?.trim() || busy === v.id}>
                      {busy === v.id ? "Generating…" : "Generate transfer code"}
                    </Button>
                  </div>
                </div>
              ) : (
                <button
                  onClick={() => setTransferring((m) => ({ ...m, [v.id]: "" }))}
                  className="text-sm text-slate-600 hover:underline">
                  Transfer ownership
                </button>
              )}
            </div>
          </Card>
        ))}
      </div>
    </>
  );
}

// expiryBadge renders an expiry timestamp with urgency colour. The
// citizen sees at a glance whether they need to renew:
//   • not on file or already past → red
//   • <= 30 days remaining → amber
//   • > 30 days remaining → green
function expiryBadge(s?: string | null) {
  if (!s) return <span className="text-red-700 font-medium">not on file</span>;
  const exp = new Date(s);
  const days = Math.floor((exp.getTime() - Date.now()) / 86400_000);
  const dateStr = exp.toISOString().slice(0, 10);
  if (days < 0) {
    return (
      <span className="text-red-700 font-medium">
        {dateStr} (expired {Math.abs(days)}d ago)
      </span>
    );
  }
  if (days <= 30) {
    return (
      <span className="text-amber-700 font-medium">
        {dateStr} ({days}d left)
      </span>
    );
  }
  return <span className="text-emerald-700">{dateStr} ({days}d left)</span>;
}
