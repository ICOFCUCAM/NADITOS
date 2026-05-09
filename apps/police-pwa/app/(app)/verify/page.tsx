"use client";

// Roadside driver-license verification.
//
// Citizen presents a QR/NFC token; we POST to /v1/licenses/verify
// and render a sun-readable standing card. Three states map 1-to-1
// to a green / amber / red command-center light, with a fallback
// for permanent suspension that overrides the colour.

import { useState } from "react";
import {
  Button, Card, EmptyState, Field, IconButton, Input, KeyValue, Mono,
  Pill, SectionHeader, services, useSession,
} from "@naditos/web-common";

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
      setErr(e?.message ?? "Verification failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="px-4 pt-4 space-y-4">
      <SectionHeader
        eyebrow="Roadside check"
        title="Verify driver"
        description="Paste / scan the citizen's license token. The system
                     returns issuing-authority-signed standing." />

      <Card pad="md" tone="elevated">
        <Field label="Driver license token"
          hint="From the citizen's My License screen — QR, NFC, or shared text.">
          <div className="flex gap-2">
            <Input
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="NADITOS-LIC-ey…"
              inputSize="lg"
              autoCorrect="off" spellCheck={false} autoCapitalize="off"
              style={{ fontFamily: "var(--ff-mono)" }}
            />
            <IconButton label="Run verification" tone="primary" size="lg"
              disabled={!token.trim() || busy} onClick={verify}>
              <ArrowRight />
            </IconButton>
          </div>
        </Field>
        {err && <p className="mt-2 text-sm text-[var(--c-bad-300)]">{err}</p>}
      </Card>

      {!result && !err && !busy && (
        <Card tone="outline" pad="md">
          <EmptyState
            title="Awaiting a token"
            description="Token verification confirms the license was issued
                         by this jurisdiction and is currently active." />
        </Card>
      )}

      {result && <StandingCard r={result} />}
    </div>
  );
}

function StandingCard({ r }: { r: Standing }) {
  const tone = r.standing === "good" ? "green" : r.standing === "watch" ? "amber" : "red";
  const TONE_CLS =
    tone === "green" ? "bg-[var(--status-ok-bg)]   ring-[var(--c-ok-500)]" :
    tone === "amber" ? "bg-[var(--status-warn-bg)] ring-[var(--c-warn-500)]" :
                       "bg-[var(--status-bad-bg)]  ring-[var(--c-bad-500)]";
  const LABEL = r.standing === "good" ? "VALID" : r.standing === "watch" ? "WATCH" : "SUSPENDED";

  return (
    <div className={`rounded-[var(--r-xl)] ring-1 ${TONE_CLS} p-5 space-y-4`}>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">License</div>
          <div className="text-2xl mt-0.5"><Mono>{r.license.license_number}</Mono></div>
          <div className="text-base text-[var(--fg-primary)] mt-1">{r.license.full_name}</div>
        </div>
        <div className={
          "inline-flex items-center rounded-[var(--r-pill)] " +
          "px-3 py-1.5 text-[11px] font-bold uppercase tracking-[0.18em] ring-1 " +
          (tone === "green" ? "bg-[var(--c-ok-500)] text-[#02150c] ring-[var(--c-ok-500)]" :
           tone === "amber" ? "bg-[var(--c-warn-500)] text-[#1a1100] ring-[var(--c-warn-500)]" :
                              "bg-[var(--c-bad-500)] text-white ring-[var(--c-bad-500)]")
        }>
          ● {LABEL}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 pt-1">
        <KeyValue k="Classes" mono v={r.license.classes.join(", ") || "—"} />
        <KeyValue k="Demerit points" v={r.license.points} />
        <KeyValue k="Issued"  v={date(r.license.issued_at)} />
        <KeyValue k="Expires" v={date(r.license.expires_at)} />
        <KeyValue k="Recent violations" v={r.recent_violations} />
        {r.license.is_suspended && (
          <KeyValue k="Suspended until" v={date(r.license.suspended_until)} />
        )}
      </div>

      {r.standing === "suspended" && (
        <div className="rounded-[var(--r-md)] bg-black text-white ring-1 ring-white/20 px-3 py-2.5
                        flex items-start gap-2">
          <span aria-hidden className="text-base leading-none">⚠</span>
          <div className="text-sm">
            License is suspended. The holder is not authorised to operate
            a motor vehicle.
          </div>
        </div>
      )}
    </div>
  );
}

function date(s?: string | null) {
  return s ? new Date(s).toISOString().slice(0, 10) : "—";
}

function ArrowRight() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>
    </svg>
  );
}
