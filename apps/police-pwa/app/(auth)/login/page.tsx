"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useSession, Button, Input } from "@naditos/web-common";

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
      setErr(e?.message ?? "Login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid place-items-center px-6">
      <form onSubmit={onSubmit} className="w-full max-w-sm space-y-3">
        <div className="text-center mb-6">
          <div className="text-3xl font-bold">NADITOS</div>
          <div className="text-sm text-slate-400">Officer enforcement</div>
        </div>
        <Input value={tenant} onChange={(e) => setTenant(e.target.value)}
          className="bg-slate-800 border-slate-700 text-white" placeholder="Tenant" />
        <Input value={email} onChange={(e) => setEmail(e.target.value)}
          className="bg-slate-800 border-slate-700 text-white" placeholder="Officer email" />
        <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
          className="bg-slate-800 border-slate-700 text-white" placeholder="Password" />
        {err && <p className="text-sm text-red-400">{err}</p>}
        <Button type="submit" disabled={busy} className="w-full bg-emerald-600 hover:bg-emerald-700">
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
    </div>
  );
}
