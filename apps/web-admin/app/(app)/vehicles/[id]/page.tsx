"use client";

// Admin vehicle detail. Read state + toggle the watch-list flags.
//
// Flags carry real consequences:
//   • is_stolen / is_seized / is_wanted feed the ANPR alert pipeline
//     and the police-PWA scan view.
// Toggling them requires confirmation.

import Link from "next/link";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { Button, Card, Pill, services, useSession,
         statusBadgeClasses, statusLabel, type VehicleStatus } from "@naditos/web-common";

type Vehicle = {
  id: string;
  plate: string;
  vin?: string | null;
  make?: string | null;
  model?: string | null;
  year?: number | null;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
  is_seized: boolean;
  is_wanted: boolean;
  owner_id?: string | null;
};

export default function VehicleDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { session } = useSession();
  const [v, setV] = useState<Vehicle | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function load() {
    if (!session || !id) return;
    try {
      const r = await services.registry(`/v1/vehicles/${id}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setV(r as Vehicle);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to load");
    }
  }
  useEffect(() => { load(); /* eslint-disable-line react-hooks/exhaustive-deps */ }, [id, session]);

  async function toggleFlag(key: "is_stolen" | "is_seized" | "is_wanted", next: boolean) {
    if (!session || !v) return;
    const labels = { is_stolen: "STOLEN", is_seized: "SEIZED", is_wanted: "WANTED" } as const;
    const verb = next ? "FLAG" : "CLEAR";
    if (!window.confirm(`${verb} ${labels[key]} on ${v.plate}? This is auditable.`)) return;
    setBusy(true);
    try {
      await services.registry(`/v1/vehicles/${v.id}/flags`, {
        method: "POST", body: { [key]: next },
        token: session.accessToken, tenant: session.user.tenant,
      });
      await load();
    } catch (e: any) {
      setErr(e?.message ?? "Flag update failed");
    } finally {
      setBusy(false);
    }
  }

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (!v) return <Card>Loading…</Card>;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">{v.plate}</h1>
        <Link href="/vehicles" className="text-sm text-slate-600 hover:underline">
          ← All vehicles
        </Link>
      </div>

      <Card>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <div className="text-slate-500">Make / model</div>
            <div>{[v.make, v.model, v.year].filter(Boolean).join(" ") || "—"}</div>
          </div>
          <div>
            <div className="text-slate-500">Status</div>
            <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs ring-1 ${statusBadgeClasses(v.status)}`}>
              {statusLabel[v.status]}
            </span>
          </div>
          <div>
            <div className="text-slate-500">Insurance expires</div>
            <div>{v.insurance_expires_at ?? "—"}</div>
          </div>
          <div>
            <div className="text-slate-500">Inspection expires</div>
            <div>{v.inspection_expires_at ?? "—"}</div>
          </div>
          {v.vin && (
            <div className="col-span-2">
              <div className="text-slate-500">VIN</div>
              <div className="font-mono text-xs">{v.vin}</div>
            </div>
          )}
        </div>
      </Card>

      <Card>
        <div className="font-semibold mb-3">Watch-list flags</div>
        <p className="text-xs text-slate-500 mb-3">
          Setting any flag triggers an ANPR alert on the next plate scan and
          shows up on the officer PWA's compliance lookup. Clearing one takes
          effect immediately. Every change is recorded in the audit log.
        </p>
        <div className="grid grid-cols-3 gap-2">
          <FlagButton label="STOLEN" on={v.is_stolen} disabled={busy}
            onSet={(n) => toggleFlag("is_stolen", n)} />
          <FlagButton label="SEIZED" on={v.is_seized} disabled={busy}
            onSet={(n) => toggleFlag("is_seized", n)} />
          <FlagButton label="WANTED" on={v.is_wanted} disabled={busy}
            onSet={(n) => toggleFlag("is_wanted", n)} />
        </div>
      </Card>
    </div>
  );
}

function FlagButton({ label, on, disabled, onSet }:
  { label: string; on: boolean; disabled: boolean; onSet: (next: boolean) => void }) {
  return (
    <div className="flex items-center justify-between rounded border border-slate-200 p-2">
      <div className="flex flex-col">
        <span className="text-xs font-medium">{label}</span>
        <Pill tone={on ? "red" : "slate"}>{on ? "ON" : "off"}</Pill>
      </div>
      <Button onClick={() => onSet(!on)} disabled={disabled}
        className={on ? "bg-slate-700 hover:bg-slate-800" : "bg-red-600 hover:bg-red-700"}>
        {on ? "Clear" : "Flag"}
      </Button>
    </div>
  );
}
