"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useSession, Button, Input, Card } from "@naditos/web-common";

export default function LoginPage() {
  const { login } = useSession();
  const router = useRouter();
  const [email, setEmail] = useState("admin@demo");
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
      router.push("/");
    } catch (e: any) {
      setErr(e?.message ?? "Login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid place-items-center px-4">
      <Card className="w-full max-w-md">
        <div className="mb-6">
          <h1 className="text-2xl font-bold tracking-tight">NADITOS</h1>
          <p className="text-sm text-slate-600">Ministry administration</p>
        </div>
        <form onSubmit={onSubmit} className="space-y-3">
          <label className="block text-sm">
            Tenant
            <Input value={tenant} onChange={(e) => setTenant(e.target.value)} />
          </label>
          <label className="block text-sm">
            Email
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </label>
          <label className="block text-sm">
            Password
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </label>
          {err && <p className="text-sm text-red-600">{err}</p>}
          <Button type="submit" disabled={busy} className="w-full">
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </Card>
    </div>
  );
}
