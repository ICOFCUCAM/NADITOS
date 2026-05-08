"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useSession, Button, Input, Card } from "@naditos/web-common";

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
      setErr(e?.message ?? "Login failed");
    } finally { setBusy(false); }
  }

  return (
    <div className="min-h-screen grid place-items-center px-4">
      <Card className="w-full max-w-md">
        <div className="mb-6">
          <h1 className="text-2xl font-bold">NADITOS</h1>
          <p className="text-sm text-slate-600">Citizen portal</p>
        </div>
        <form onSubmit={onSubmit} className="space-y-3">
          <Input value={tenant}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setTenant(e.target.value)}
            placeholder="Tenant / country" />
          <Input value={email}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setEmail(e.target.value)}
            placeholder="Email" />
          <Input type="password" value={password}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPassword(e.target.value)}
            placeholder="Password" />
          {err && <p className="text-sm text-red-600">{err}</p>}
          <Button type="submit" disabled={busy} className="w-full">
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </Card>
    </div>
  );
}
