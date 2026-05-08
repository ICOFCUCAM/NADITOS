"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Health = {
  module: string;
  provider: string;
  state: "ok" | "degraded" | "down" | "unknown";
  fail_streak: number;
  last_ok_at?: string | null;
  last_fail_at?: string | null;
};

const TONES: Record<Health["state"], "green" | "amber" | "red" | "slate"> = {
  ok: "green", degraded: "amber", down: "red", unknown: "slate",
};

export default function ProvidersPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Health[]>([]);

  useEffect(() => {
    if (!session) return;
    Promise.all([
      services.insurance("/v1/insurance/health", {
        token: session.accessToken, tenant: session.user.tenant,
      }).catch(() => null),
      services.inspection("/v1/inspection/health", {
        token: session.accessToken, tenant: session.user.tenant,
      }).catch(() => null),
    ]).then((rs) => {
      setItems(rs.filter(Boolean) as Health[]);
    });
  }, [session]);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Provider health</h1>
        <p className="text-sm text-slate-600">
          External integrations (insurance, inspection, payments, courts,
          ANPR) self-report health. Streak ≥ 5 → state=down.
        </p>
      </div>

      <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {items.map((h) => (
          <Card key={`${h.module}-${h.provider}`}>
            <div className="flex justify-between items-start">
              <div>
                <div className="text-xs uppercase text-slate-500">{h.module}</div>
                <div className="font-mono">{h.provider}</div>
              </div>
              <Pill tone={TONES[h.state]}>{h.state}</Pill>
            </div>
            <div className="mt-3 text-xs text-slate-500 space-y-1">
              <div>Fail streak: {h.fail_streak}</div>
              <div>Last OK: {h.last_ok_at ? new Date(h.last_ok_at).toLocaleString() : "—"}</div>
              <div>Last fail: {h.last_fail_at ? new Date(h.last_fail_at).toLocaleString() : "—"}</div>
            </div>
          </Card>
        ))}
        {items.length === 0 && (
          <Card>No provider health snapshots yet. Trigger a verify call to populate.</Card>
        )}
      </div>
    </div>
  );
}
