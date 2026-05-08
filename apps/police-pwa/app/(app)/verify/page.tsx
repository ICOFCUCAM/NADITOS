"use client";

// Roadside driver-license verification.
//
// Flow: citizen presents a QR/NFC token (issued via
// POST /v1/licenses/{id}/issue-token in the citizen portal). The
// officer pastes / scans the token here, we POST it to
// /v1/licenses/verify, and render the standing card.
//
// `standing` comes from v_driver_standing in the license service:
//   "good"        — no active suspension, no recent demerits over threshold
//   "watch"       — recent demerits but still under threshold
//   "suspended"   — active driver_suspension row (license is invalid)
//
// Any non-"good" result must be visually loud. Officers run this from
// the side of the road with bright sun on the screen.

import { useState } from "react";
import { Button, Input, services, useSession } from "@naditos/web-common";

type License = {
  id: string;
  license_number: string;
  full_name: string;
  classes: string[];
  issued_at: string;
  expires_at: string;
  is_suspended: boolean;
  suspended_until?: string | null;
  points: number;
};

type Standing = {
  license: License;
  standing: "good" | "watch" | "suspended";
  recent_violations: number;
};

const STANDING_TONE: Record<Standing["standing"], string> = {
  good:      "bg-emerald-900/50 border-emerald-700 text-emerald-200",
  watch:     "bg-amber-900/50 border-amber-700 text-amber-200",
  suspended: "bg-red-900/50 border-red-700 text-red-200",
};

const STANDING_LABEL: Record<Standing["standing"], string> = {
  good:      "VALID",
  watch:     "WATCH",
  suspended: "SUSPENDED",
};

export default function VerifyPage() {
  const { session } = useSession();
  const [token, setToken] = useState("");
  const [result, setResult] = useState<Standing | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function verify() {
    if (!session || !token.trim()) return;
    setBusy(true);
    setErr(null);
    setResult(null);
    try {
      const r = await services.license(`/v1/licenses/verify`, {
        method: "POST",
        token: session.accessToken, tenant: session.user.tenant,
        body: { token: token.trim() },
      });
      setResult(r as Standing);
    } catch (e: any) {
      // 400 bad_token / 403 wrong tenant / 404 license unknown — all
      // map to "could not verify". Officer falls back to manual ID.
      setErr(e?.message ?? "Verification failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="p-4 space-y-4">
      <div className="space-y-2">
        <label className="block text-xs uppercase tracking-wide text-slate-400">
          Driver license token
        </label>
        <Input
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="paste from citizen QR / NFC"
          className="bg-slate-800 border-slate-700 text-white font-mono"
        />
        <Button
          onClick={verify} disabled={!token.trim() || busy}
          className="w-full bg-emerald-600 hover:bg-emerald-700">
          {busy ? "Verifying…" : "Verify license"}
        </Button>
        {err && <p className="text-sm text-red-400">{err}</p>}
      </div>

      {result && (
        <div className={`rounded-lg p-4 space-y-2 border ${STANDING_TONE[result.standing]}`}>
          <div className="flex items-center justify-between">
            <div className="text-2xl font-mono">{result.license.license_number}</div>
            <span className="text-sm font-bold tracking-wider">
              {STANDING_LABEL[result.standing]}
            </span>
          </div>
          <div className="text-sm text-slate-200">{result.license.full_name}</div>
          <div className="grid grid-cols-2 gap-2 text-xs text-slate-300">
            <div>Classes: <span className="font-mono">{result.license.classes.join(", ") || "—"}</span></div>
            <div>Points: <span className="font-mono">{result.license.points}</span></div>
            <div>Issued: <span className="font-mono">{date(result.license.issued_at)}</span></div>
            <div>Expires: <span className="font-mono">{date(result.license.expires_at)}</span></div>
            <div>Recent violations: <span className="font-mono">{result.recent_violations}</span></div>
            {result.license.is_suspended && (
              <div>Suspended until: <span className="font-mono">{date(result.license.suspended_until)}</span></div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function date(s?: string | null) {
  return s ? new Date(s).toISOString().slice(0, 10) : "—";
}
