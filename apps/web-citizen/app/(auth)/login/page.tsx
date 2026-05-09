"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Field, Input, useSession } from "@naditos/web-common";

// Citizen sign-in. Document-feel: a calm card on the light surface,
// the same NADITOS mark and structure as the other apps so trust
// transfers between contexts.

export default function LoginPage() {
  const { login } = useSession();
  const router = useRouter();
  const [email, setEmail] = useState("citizen@demo");
  const [password, setPassword] = useState("demo1234");
  const [tenant, setTenant] = useState("demo");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr(null);
    try {
      await login(email, password, tenant);
      router.push("/");
    } catch (e: any) {
      setErr(e?.message ?? "Sign-in failed");
    } finally { setBusy(false); }
  }

  return (
    <div className="min-h-screen grid place-items-center px-4 py-12">
      <div className="w-full max-w-sm space-y-5">
        <div className="text-center">
          <span aria-hidden
            className="inline-grid h-12 w-12 place-items-center rounded-[var(--r-lg)]
                       bg-[var(--accent-primary)] text-[var(--accent-primary-fg)]
                       font-bold text-lg"
            style={{ fontFamily: "var(--ff-display)", boxShadow: "var(--glow-ops)" }}>
            N
          </span>
          <h1 className="mt-3 text-2xl font-semibold tracking-tight"
              style={{ fontFamily: "var(--ff-display)" }}>NADITOS</h1>
          <p className="text-[12px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mt-1">
            Citizen portal
          </p>
        </div>

        <form onSubmit={onSubmit}
          className="rounded-[var(--r-xl)] bg-[var(--bg-surface)] ring-1 ring-[var(--border-subtle)]
                     shadow-[var(--e-raised)] p-6 space-y-4">
          <Field label="Country / authority">
            <Input value={tenant}
              onChange={(e) => setTenant(e.target.value)}
              inputSize="lg" autoComplete="organization" />
          </Field>
          <Field label="Email">
            <Input value={email}
              onChange={(e) => setEmail(e.target.value)}
              inputSize="lg" autoComplete="username" type="email" />
          </Field>
          <Field label="Password" error={err}>
            <Input type="password" value={password}
              onChange={(e) => setPassword(e.target.value)}
              inputSize="lg" autoComplete="current-password"
              invalid={Boolean(err)} />
          </Field>
          <Button type="submit" disabled={busy} fullWidth size="lg" tone="primary">
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>

        <p className="text-center text-[11px] text-[var(--fg-muted)] tracking-wide">
          By signing in you accept the records-of-handling notice. Your
          interactions with the system are sealed in an audit ledger.
        </p>
      </div>
    </div>
  );
}
