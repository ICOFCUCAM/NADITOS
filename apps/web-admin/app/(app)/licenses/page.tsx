"use client";

import { useEffect, useState } from "react";
import { Card, Input, Pill, Button, services, useSession } from "@naditos/web-common";

type LicenseStanding = {
  license: {
    id: string; license_number: string; full_name: string; classes: string[];
    issued_at?: string | null; expires_at?: string | null;
    points: number; is_suspended: boolean; suspended_until?: string | null;
  };
  standing: "good" | "expiring_soon" | "at_risk" | "expired" | "suspended";
  recent_violations: number;
  next_suspension_threshold: number;
};

const STANDING_TONES: Record<LicenseStanding["standing"], "green" | "amber" | "red" | "black"> = {
  good: "green",
  expiring_soon: "amber",
  at_risk: "amber",
  expired: "red",
  suspended: "black",
};

type SuspendedRow = {
  id: string; license_number: string; full_name: string;
  is_suspended: boolean; suspended_until?: string | null;
};

export default function LicensesPage() {
  const { session } = useSession();
  const [q, setQ] = useState("");
  const [result, setResult] = useState<LicenseStanding | null>(null);
  const [violations, setViolations] = useState<any[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [suspended, setSuspended] = useState<SuspendedRow[]>([]);

  // Pre-populate the suspended list on mount so admins land on a
  // useful triage screen without having to know a specific number.
  useEffect(() => {
    if (!session) return;
    services.license("/v1/licenses?suspended=true", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setSuspended(r.items ?? []))
      .catch(() => {});
  }, [session]);

  async function lookup() {
    if (!session || !q) return;
    setBusy(true);
    setErr(null);
    try {
      const lic: any = await services.license(`/v1/licenses?number=${encodeURIComponent(q)}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      const standing: LicenseStanding = await services.license(`/v1/licenses/${lic.id}/standing`, {
        token: session.accessToken, tenant: session.user.tenant,
      }) as LicenseStanding;
      const v: any = await services.license(`/v1/licenses/${lic.id}/violations`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setResult(standing);
      setViolations(v.items ?? []);
    } catch (e: any) {
      setResult(null);
      setViolations([]);
      setErr(e?.message ?? "Lookup failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Driver licenses</h1>
        <p className="text-sm text-slate-600">
          Lookup by license number. Standing reflects the live demerit ledger
          and active suspensions.
        </p>
      </div>

      <Card>
        <div className="flex gap-2 items-end">
          <div className="flex-1">
            <label className="block text-xs uppercase text-slate-500 mb-1">License number</label>
            <Input value={q} onChange={(e) => setQ(e.target.value.toUpperCase())}
              placeholder="DL-12345" className="font-mono" />
          </div>
          <Button onClick={lookup} disabled={busy || !q}>
            {busy ? "Looking up…" : "Lookup"}
          </Button>
        </div>
        {err && <p className="text-sm text-red-600 mt-2">{err}</p>}
      </Card>

      {!result && suspended.length > 0 && (
        <Card className="p-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-slate-100 flex items-center justify-between">
            <div className="font-semibold">Active suspensions</div>
            <Pill tone="red">{suspended.length}</Pill>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-600">
              <tr>
                <th className="text-left p-3">Number</th>
                <th className="text-left p-3">Holder</th>
                <th className="text-left p-3">Suspended until</th>
                <th className="p-3"></th>
              </tr>
            </thead>
            <tbody>
              {suspended.map((s) => (
                <tr key={s.id} className="border-t border-slate-100">
                  <td className="p-3 font-mono">{s.license_number}</td>
                  <td className="p-3">{s.full_name}</td>
                  <td className="p-3">{s.suspended_until ?? "—"}</td>
                  <td className="p-3 text-right">
                    <button
                      onClick={() => { setQ(s.license_number); setTimeout(lookup, 0); }}
                      className="text-xs underline text-slate-700">
                      Inspect
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {result && (
        <div className="grid md:grid-cols-2 gap-4">
          <Card>
            <div className="flex justify-between items-start">
              <div>
                <div className="text-sm text-slate-500">License</div>
                <div className="text-xl font-mono">{result.license.license_number}</div>
              </div>
              <Pill tone={STANDING_TONES[result.standing]}>{result.standing}</Pill>
            </div>
            <div className="mt-3 space-y-1 text-sm">
              <div>Holder: <span className="font-medium">{result.license.full_name}</span></div>
              <div>Classes: <span className="font-mono">{result.license.classes.join(", ")}</span></div>
              <div>Issued: {result.license.issued_at ?? "—"}</div>
              <div>Expires: {result.license.expires_at ?? "—"}</div>
            </div>
          </Card>
          <Card>
            <div className="text-sm text-slate-500">Demerit standing</div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-3xl font-bold">{result.license.points}</span>
              <span className="text-slate-500">/ {result.next_suspension_threshold} suspension threshold</span>
            </div>
            <div className="mt-3 h-2 rounded-full bg-slate-100 overflow-hidden">
              <div className={`h-full ${result.license.points >= result.next_suspension_threshold ? "bg-red-500" : "bg-amber-500"}`}
                style={{ width: `${Math.min(100, (result.license.points / Math.max(1, result.next_suspension_threshold)) * 100)}%` }} />
            </div>
            <div className="mt-3 text-sm text-slate-600">
              Recent violations (24m): <span className="font-medium">{result.recent_violations}</span>
            </div>
            {result.license.is_suspended && (
              <div className="mt-3 rounded bg-slate-900 text-white p-3 text-sm space-y-2">
                <div>
                  Suspended {result.license.suspended_until && `until ${result.license.suspended_until}`}
                </div>
                <Button
                  onClick={async () => {
                    if (!session) return;
                    if (!window.confirm("Lift the active suspension on " + result.license.license_number + "?")) return;
                    try {
                      const sid: any = await services.license(
                        `/v1/licenses/${result.license.id}/suspensions/active`, {
                        token: session.accessToken, tenant: session.user.tenant,
                      });
                      await services.license(
                        `/v1/licenses/${result.license.id}/suspensions/${sid.id}/lift`, {
                        method: "POST",
                        token: session.accessToken, tenant: session.user.tenant,
                      });
                      await lookup();
                    } catch (e: any) {
                      setErr(e?.message ?? "Lift failed");
                    }
                  }}
                  className="bg-amber-500 hover:bg-amber-600 text-slate-900">
                  Lift suspension
                </Button>
              </div>
            )}
          </Card>
        </div>
      )}

      {result && violations.length > 0 && (
        <Card className="p-0 overflow-hidden">
          <div className="bg-slate-50 px-4 py-2 text-sm font-medium">Violation history</div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-600">
              <tr>
                <th className="text-left p-3">When</th>
                <th className="text-left p-3">Offence</th>
                <th className="text-left p-3">Points</th>
                <th className="text-left p-3">Fine</th>
              </tr>
            </thead>
            <tbody>
              {violations.map((v) => (
                <tr key={v.id} className="border-t border-slate-100">
                  <td className="p-3">{new Date(v.occurred_at).toLocaleString()}</td>
                  <td className="p-3 font-mono">{v.offence_code}</td>
                  <td className="p-3">{v.points}</td>
                  <td className="p-3 font-mono text-xs">{v.fine_id?.slice(0, 8) ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}
