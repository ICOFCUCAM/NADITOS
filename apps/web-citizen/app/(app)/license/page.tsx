"use client";

// Citizen license page.
//
// Server returns:
//   { license: {...}, standing, recent_violations, demerits[], suspensions[] }
//
// Standing tone:
//   good        — green
//   expiring_soon / at_risk / watch — amber
//   expired / suspended            — red

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type License = {
  id: string;
  license_number: string;
  full_name: string;
  classes: string[];
  issued_at: string;
  expires_at: string;
  points: number;
  is_suspended: boolean;
  suspended_until?: string | null;
};

type Demerit = {
  occurred_at: string;
  delta: number;
  reason: string;
  source: string;
  new_total: number;
};

type Suspension = {
  id: string;
  reason: string;
  starts_at: string;
  ends_at: string;
  lifted_at?: string | null;
  trigger_kind: string;
};

type Bundle = {
  license: License;
  standing: string;
  recent_violations: number;
  demerits: Demerit[];
  suspensions: Suspension[];
};

const STANDING_TONE: Record<string, "green" | "amber" | "red" | "slate"> = {
  good: "green",
  watch: "amber",
  expiring_soon: "amber",
  at_risk: "amber",
  expired: "red",
  suspended: "red",
};

export default function MyLicensePage() {
  const { session } = useSession();
  const [data, setData] = useState<Bundle | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    if (!session) return;
    services.license("/v1/citizens/me/license", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setData(r as Bundle))
      .catch((e: any) => {
        // 404 is not an error here — citizen just doesn't have a
        // license issued in this tenant.
        if (e?.message?.includes("404")) setMissing(true);
        else setErr(e?.message ?? "Failed to load");
      });
  }, [session]);

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (missing) {
    return (
      <>
        <h1 className="text-2xl font-bold">My driver license</h1>
        <Card>
          <p className="text-sm text-slate-600">
            No driver license is on file for your account in this jurisdiction.
            If you believe this is wrong, contact your local transport ministry.
          </p>
        </Card>
      </>
    );
  }
  if (data === null) return <Card>Loading…</Card>;

  const tone = STANDING_TONE[data.standing] ?? "slate";

  return (
    <>
      <h1 className="text-2xl font-bold">My driver license</h1>

      <Card>
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <div className="font-mono text-xl">{data.license.license_number}</div>
            <div className="text-sm text-slate-700">{data.license.full_name}</div>
            <div className="text-xs text-slate-500">
              Classes: <span className="font-mono">{data.license.classes.join(", ") || "—"}</span>
            </div>
          </div>
          <Pill tone={tone}>{data.standing}</Pill>
        </div>
        <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-500">
          <div>Issued: <span className="text-slate-800">{date(data.license.issued_at)}</span></div>
          <div>Expires: <span className="text-slate-800">{date(data.license.expires_at)}</span></div>
          <div>Points: <span className="text-slate-800 font-mono">{data.license.points}</span></div>
          <div>Recent violations: <span className="text-slate-800 font-mono">{data.recent_violations}</span></div>
        </div>
        {data.license.is_suspended && (
          <div className="mt-3 rounded bg-red-50 border border-red-200 p-2 text-sm text-red-800">
            License suspended {data.license.suspended_until ? `until ${date(data.license.suspended_until)}` : ""}.
            Contact your local transport ministry for the reinstatement procedure.
          </div>
        )}
      </Card>

      {data.demerits.length > 0 && (
        <>
          <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-4">
            Demerit history
          </h2>
          <Card className="p-0 overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-slate-50 text-slate-600">
                <tr>
                  <th className="text-left p-2">When</th>
                  <th className="text-left p-2">Δ</th>
                  <th className="text-left p-2">Reason</th>
                  <th className="text-left p-2">Total</th>
                </tr>
              </thead>
              <tbody>
                {data.demerits.map((d, i) => (
                  <tr key={i} className="border-t border-slate-100">
                    <td className="p-2 text-xs">{new Date(d.occurred_at).toLocaleDateString()}</td>
                    <td className="p-2 font-mono">+{d.delta}</td>
                    <td className="p-2">{d.reason}</td>
                    <td className="p-2 font-mono">{d.new_total}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </>
      )}

      {data.suspensions.length > 0 && (
        <>
          <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-4">
            Suspension history
          </h2>
          {data.suspensions.map((s) => (
            <Card key={s.id}>
              <div className="flex items-start justify-between gap-3">
                <div className="space-y-1">
                  <div className="text-sm">{s.reason}</div>
                  <div className="text-xs text-slate-500">
                    {date(s.starts_at)} → {date(s.ends_at)}
                    {s.lifted_at && <> · lifted {date(s.lifted_at)}</>}
                  </div>
                </div>
                <Pill tone={s.lifted_at ? "slate" : "red"}>
                  {s.lifted_at ? "lifted" : "active"}
                </Pill>
              </div>
            </Card>
          ))}
        </>
      )}
    </>
  );
}

function date(s?: string | null) {
  return s ? new Date(s).toISOString().slice(0, 10) : "—";
}
