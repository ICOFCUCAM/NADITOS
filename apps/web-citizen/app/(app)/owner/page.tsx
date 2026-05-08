"use client";

import { useState } from "react";
import { Button, Card, Input, Pill, services, useSession } from "@naditos/web-common";

// /v1/citizens/me/owner is idempotent — the same request submitted
// twice updates the record rather than creating a duplicate. The page
// always shows the form; if a profile already exists the same submit
// updates it.
export default function OwnerPage() {
  const { session } = useSession();
  const [fullName, setFullName] = useState(session?.user.full_name ?? "");
  const [email, setEmail] = useState(session?.user.email ?? "");
  const [phone, setPhone] = useState("");
  const [nationalID, setNationalID] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [savedID, setSavedID] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!session) return;
    setBusy(true);
    setErr(null);
    try {
      const body: Record<string, unknown> = { full_name: fullName };
      if (email)      body.email = email;
      if (phone)      body.phone = phone;
      if (nationalID) body.national_id = nationalID;

      const r: any = await services.registry("/v1/citizens/me/owner", {
        method: "POST",
        token: session.accessToken,
        tenant: session.user.tenant,
        body,
      });
      setSavedID(r.id);
    } catch (e: any) {
      setErr(e?.message ?? "Save failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1 className="text-2xl font-bold">My profile</h1>
      <p className="text-sm text-slate-600">
        This is the record we use to send fine notices, payment receipts,
        and renewal reminders. You only need to fill it once — submitting
        again later updates your contact info.
      </p>

      <Card>
        <form onSubmit={submit} className="space-y-3">
          <label className="block text-sm">
            Full name
            <Input value={fullName}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setFullName(e.target.value)}
              required />
          </label>
          <label className="block text-sm">
            Email
            <Input type="email" value={email}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setEmail(e.target.value)} />
          </label>
          <label className="block text-sm">
            Phone
            <Input value={phone}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPhone(e.target.value)}
              placeholder="+1 555 0100" />
          </label>
          <label className="block text-sm">
            National ID <span className="text-slate-500 text-xs">(optional)</span>
            <Input value={nationalID}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setNationalID(e.target.value)} />
          </label>
          {err && <p className="text-sm text-red-600">{err}</p>}
          <div className="flex items-center gap-3 pt-2">
            <Button type="submit" disabled={busy || !fullName}>
              {busy ? "Saving…" : "Save profile"}
            </Button>
            {savedID && (
              <Pill tone="green">Profile saved · {savedID.slice(0, 8)}</Pill>
            )}
          </div>
        </form>
      </Card>

      <Card>
        <div className="text-sm text-slate-600">
          The platform looks up <strong>your email</strong> first, falling back to
          phone, when sending you notifications. Make sure at least one is
          set so you receive fine notices.
        </div>
      </Card>
    </>
  );
}
