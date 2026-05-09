"use client";

// Officer self-stats + sign out.
//
// /v1/audit/officers/me/stats returns the caller's own
// officer_daily_stats rows for the last 14 days. The data is what
// the rollup recomputes hourly; an officer sees their own activity
// without holding admin perms.

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Button, services, useSession } from "@naditos/web-common";

type Day = {
  day: string;
  fines_issued: number;
  fines_cancelled: number;
  fines_total: string;
  unique_plates: number;
  anomaly_score: number | null;
};

export default function MePage() {
  const { session, logout } = useSession();
  const router = useRouter();
  const [days, setDays] = useState<Day[] | null>(null);

  useEffect(() => {
    if (!session) return;
    services.audit("/v1/audit/officers/me/stats", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setDays(r.items ?? []))
      .catch(() => setDays([]));
  }, [session]);

  // Sum the last 14 days for the headline numbers.
  const totals = (days ?? []).reduce(
    (acc, d) => ({
      issued: acc.issued + d.fines_issued,
      cancelled: acc.cancelled + d.fines_cancelled,
      plates: acc.plates + d.unique_plates,
    }),
    { issued: 0, cancelled: 0, plates: 0 },
  );

  return (
    <div className="p-4 space-y-4">
      <h1 className="text-xl font-bold">{session?.user.full_name}</h1>
      <p className="text-xs text-slate-400">
        {session?.user.email} · {session?.user.role}
      </p>

      <div className="grid grid-cols-3 gap-2">
        <Stat label="Issued (14d)" value={totals.issued} />
        <Stat label="Cancelled" value={totals.cancelled} />
        <Stat label="Plates" value={totals.plates} />
      </div>

      <h2 className="text-xs uppercase tracking-wide text-slate-400 mt-4">
        Daily breakdown
      </h2>
      {days === null && <p className="text-sm text-slate-400">Loading…</p>}
      {days !== null && days.length === 0 && (
        <p className="text-sm text-slate-400">No fines issued in the last 14 days.</p>
      )}
      {days?.map((d) => (
        <div key={d.day} className="rounded bg-slate-800 p-2 text-sm flex justify-between items-center">
          <div>
            <div className="font-medium">{new Date(d.day).toLocaleDateString()}</div>
            <div className="text-xs text-slate-400">
              {d.fines_issued} issued · {d.fines_cancelled} cancelled · {d.unique_plates} plates
            </div>
          </div>
          {d.anomaly_score != null && d.anomaly_score > 2 && (
            <span className="text-xs text-red-400 font-mono">
              z={d.anomaly_score.toFixed(2)}
            </span>
          )}
        </div>
      ))}

      <Button
        onClick={() => logout().then(() => router.replace("/login"))}
        className="w-full bg-red-600 hover:bg-red-700 mt-6">
        Sign out
      </Button>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded bg-slate-800 p-2 text-center">
      <div className="text-xl font-bold">{value}</div>
      <div className="text-[10px] uppercase tracking-wide text-slate-400">{label}</div>
    </div>
  );
}
