"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  Card, Pill, services, useSession,
  statusBadgeClasses, statusLabel, type VehicleStatus,
} from "@naditos/web-common";

type Vehicle = {
  id: string;
  plate: string;
  make?: string | null;
  model?: string | null;
  year?: number | null;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
};

export default function MyVehiclesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Vehicle[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!session) return;
    services.registry("/v1/citizens/me/vehicles", {
      token: session.accessToken,
      tenant: session.user.tenant,
    })
      .then((r: any) => setItems(r.items ?? []))
      .catch((e) => setErr(e?.message ?? "Failed to load"));
  }, [session]);

  if (err) {
    return (
      <Card className="text-red-700">
        Couldn't load your vehicles: {err}
      </Card>
    );
  }
  if (items === null) {
    return <Card>Loading…</Card>;
  }
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
            <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-500">
              <div>Insurance: <span className="text-slate-800">{date(v.insurance_expires_at)}</span></div>
              <div>Inspection: <span className="text-slate-800">{date(v.inspection_expires_at)}</span></div>
            </div>
            {v.is_stolen && <Pill tone="black">stolen</Pill>}
          </Card>
        ))}
      </div>
    </>
  );
}

function date(s?: string | null) {
  return s ? new Date(s).toISOString().slice(0, 10) : "—";
}
