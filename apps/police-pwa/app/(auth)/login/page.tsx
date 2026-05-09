"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Field, Input, useSession } from "@naditos/web-common";

// Officer sign-in.
//
// This is the first impression of NADITOS for any officer in the
// field, so the surface is intentionally institutional: a sealed-
// looking command panel, a sovereign mark, and the operating
// jurisdiction (tenant) on the left so an officer signing in to a
// shared device sees immediately which authority they're about to
// authenticate against.

export default function LoginPage() {
  const { login } = useSession();
  const router = useRouter();
  const [email, setEmail] = useState("officer@demo");
  const [password, setPassword] = useState("demo1234");
  const [tenant, setTenant] = useState("demo");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await login(email, password, tenant);
      router.push("/scan");
    } catch (e: any) {
      setErr(e?.message ?? "Sign-in failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen relative grid lg:grid-cols-[1.1fr_minmax(380px,1fr)]">
      {/* Decorative ops panel — hidden on small screens, visible from lg+ */}
      <aside
        aria-hidden
        className="relative hidden lg:flex items-end p-12 overflow-hidden
                   border-r border-[var(--border-subtle)]
                   bg-[var(--bg-surface)]"
      >
        <div className="absolute inset-0 nadit-radar-grid opacity-50" />
        <div
          className="absolute inset-0"
          style={{
            background:
              "radial-gradient(700px 400px at 30% 30%, rgba(34,211,238,0.08), transparent 60%)," +
              "radial-gradient(900px 500px at 80% 80%, rgba(245,184,0,0.05), transparent 60%)",
          }}
        />
        <div className="relative z-10 max-w-lg space-y-6">
          <div className="inline-flex items-center gap-2 rounded-full
                          ring-1 ring-[var(--border-default)] bg-[var(--bg-elevated)]/60
                          px-3 py-1 text-[11px] uppercase tracking-[0.18em]
                          text-[var(--fg-muted)]">
            <span className="h-1.5 w-1.5 rounded-full bg-[var(--accent-primary)]" />
            National operations · Officer terminal
          </div>
          <h1
            className="text-[clamp(2.4rem,1.6rem+3vw,3.8rem)] leading-[0.95]
                       text-[var(--fg-primary)] font-semibold tracking-tight"
            style={{ fontFamily: "var(--ff-display)" }}
          >
            Sovereign transport <br/>
            <span className="text-[var(--accent-primary)]">enforcement network.</span>
          </h1>
          <p className="text-[var(--fg-secondary)] max-w-md leading-relaxed">
            Verified plate scans, immediate compliance lookup, and a sealed
            chain of custody from capture to court. Authenticate to begin a
            shift.
          </p>
          <div className="grid grid-cols-3 gap-3 pt-4">
            <Stat k="Anti-tamper" v="Hash-chained" />
            <Stat k="Network" v="Offline-aware" />
            <Stat k="Compliance" v="ISO-grade" />
          </div>
        </div>
      </aside>

      {/* Sign-in card */}
      <main className="flex items-center justify-center p-6 sm:p-12">
        <form
          onSubmit={onSubmit}
          className="w-full max-w-sm space-y-5 rounded-[var(--r-xl)]
                     bg-[var(--bg-surface)] ring-1 ring-[var(--border-subtle)]
                     shadow-[var(--e-floating)] p-6 sm:p-8"
        >
          <div className="flex items-center gap-3">
            <span
              aria-hidden
              className="h-10 w-10 rounded-[var(--r-md)]
                         bg-[var(--accent-primary)] text-[var(--accent-primary-fg)]
                         grid place-items-center font-bold text-base tracking-tighter"
              style={{
                fontFamily: "var(--ff-display)",
                boxShadow: "var(--glow-ops)",
              }}
            >
              N
            </span>
            <div className="leading-tight">
              <div className="text-[15px] font-semibold tracking-[0.04em] text-[var(--fg-primary)]"
                   style={{ fontFamily: "var(--ff-display)" }}>
                NADITOS
              </div>
              <div className="text-[11px] text-[var(--fg-muted)] tracking-wider uppercase">
                Officer · Field workspace
              </div>
            </div>
          </div>

          <div className="space-y-3 pt-1">
            <Field label="Operating jurisdiction"
              hint="Set by your department; your token stays scoped to it.">
              <Input value={tenant} onChange={(e) => setTenant(e.target.value)}
                inputSize="lg" autoComplete="organization" placeholder="ministry-id" />
            </Field>
            <Field label="Officer email">
              <Input value={email} onChange={(e) => setEmail(e.target.value)}
                inputSize="lg" autoComplete="username" inputMode="email"
                placeholder="officer@authority.gov" />
            </Field>
            <Field label="Password" error={err}>
              <Input type="password" value={password}
                onChange={(e) => setPassword(e.target.value)}
                inputSize="lg" autoComplete="current-password"
                invalid={Boolean(err)} placeholder="••••••••" />
            </Field>
          </div>

          <Button type="submit" disabled={busy} fullWidth size="lg" tone="primary">
            {busy ? "Authenticating…" : "Sign in to begin shift"}
          </Button>

          <div className="pt-2 text-[11px] text-[var(--fg-muted)] tracking-wide leading-relaxed">
            Authorized personnel only. All actions are recorded in an
            append-only audit ledger and may be reviewed by court order.
          </div>
        </form>
      </main>
    </div>
  );
}

function Stat({ k, v }: { k: string; v: string }) {
  return (
    <div className="rounded-[var(--r-md)] bg-[var(--bg-elevated)]/60
                    ring-1 ring-[var(--border-subtle)] px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">{k}</div>
      <div className="text-sm text-[var(--fg-primary)] font-medium">{v}</div>
    </div>
  );
}
