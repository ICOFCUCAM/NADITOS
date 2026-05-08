"use client";

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

// Some modules track operational state via the HealthMonitor (insurance,
// inspection, payments, courts) and report fail_streak / last_ok_at /
// last_fail_at. Others (ANPR — phase-3) only report which provider is
// currently bound. The page renders both shapes without forcing a
// pretend "state" onto modules that don't measure it yet.
type Health = {
  module: string;
  provider: string;
  state?: "ok" | "degraded" | "down" | "unknown";
  fail_streak?: number;
  last_ok_at?: string | null;
  last_fail_at?: string | null;
  region?: string;
};

const TONES: Record<NonNullable<Health["state"]> | "wired", "green" | "amber" | "red" | "slate"> = {
  ok: "green", degraded: "amber", down: "red", unknown: "slate", wired: "slate",
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
      services.anpr("/v1/anpr/health", {
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
          External integrations report which adapter is wired and (for
          modules that track verifies) their operational state. Streak ≥ 5
          flips a provider's state to <code>down</code>; anything below
          shows as <code>degraded</code>.
        </p>
      </div>

      <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {items.map((h) => {
          const tone = TONES[h.state ?? "wired"];
          const label = h.state ?? "wired";
          return (
            <Card key={`${h.module}-${h.provider}`}>
              <div className="flex justify-between items-start">
                <div>
                  <div className="text-xs uppercase text-slate-500">{h.module}</div>
                  <div className="font-mono">{h.provider}</div>
                </div>
                <Pill tone={tone}>{label}</Pill>
              </div>
              <div className="mt-3 text-xs text-slate-500 space-y-1">
                {h.region && <div>Region: {h.region}</div>}
                {h.state !== undefined ? (
                  <>
                    <div>Fail streak: {h.fail_streak ?? 0}</div>
                    <div>Last OK: {h.last_ok_at ? new Date(h.last_ok_at).toLocaleString() : "—"}</div>
                    <div>Last fail: {h.last_fail_at ? new Date(h.last_fail_at).toLocaleString() : "—"}</div>
                  </>
                ) : (
                  <div className="italic">Adapter wired; no verifies recorded yet.</div>
                )}
              </div>
            </Card>
          );
        })}
        {items.length === 0 && (
          <Card>
            No provider health snapshots yet. Trigger a verify call to
            populate (insurance / inspection) or check that the ANPR
            service is up.
          </Card>
        )}
      </div>
    </div>
  );
}
