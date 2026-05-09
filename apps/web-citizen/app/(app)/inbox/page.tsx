"use client";

// Citizen notifications inbox.
//
// Server returns all sent messages whose recipient string matches
// the citizen's email or phone (looked up server-side from
// users + owners). This is a read-only mirror of the SMS/email
// the platform sent — useful when a citizen has lost the SMS or
// missed the email.

import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Message = {
  id: string;
  channel: string;
  recipient: string;
  subject: string;
  template: string | null;
  status: string;
  body: string;
  created_at: string;
  sent_at: string | null;
};

const TEMPLATE_LABEL: Record<string, string> = {
  "fine.issued.v1":          "Fine issued",
  "fine.paid.v1":            "Payment receipt",
  "fine.escalated.v1":       "Fine escalated",
  "license.demerit.v1":      "Demerit points added",
  "license.suspended.v1":    "License suspended",
  "license.reinstated.v1":   "License reinstated",
  "vehicle.transferred.v1":  "Vehicle transferred to you",
};

export default function InboxPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Message[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!session) return;
    services.notify("/v1/citizens/me/notifications", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setItems(r.items ?? []))
      .catch((e: any) => setErr(e?.message ?? "Failed to load"));
  }, [session]);

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (items === null) return <Card>Loading…</Card>;
  if (items.length === 0) {
    return (
      <>
        <h1 className="text-2xl font-bold">Notifications</h1>
        <Card>
          <p className="text-sm text-slate-600">
            Nothing in your inbox yet. Notices about fines, demerit points,
            license status, and vehicle transfers will appear here as they're sent.
          </p>
          <p className="mt-2 text-xs text-slate-500">
            Make sure your <a href="/owner" className="underline">profile</a>{" "}
            email/phone is up to date — the platform matches messages by those.
          </p>
        </Card>
      </>
    );
  }

  return (
    <>
      <h1 className="text-2xl font-bold">Notifications</h1>
      <p className="text-sm text-slate-600">
        Messages the platform sent to your registered email or phone.
      </p>
      {items.map((m) => (
        <Card key={m.id}>
          <div className="flex items-start justify-between gap-3">
            <div className="space-y-1 min-w-0">
              <div className="text-sm font-semibold">
                {m.subject || TEMPLATE_LABEL[m.template ?? ""] || "(no subject)"}
              </div>
              <div className="text-xs text-slate-500">
                {new Date(m.created_at).toLocaleString()}
                {" · via "}{m.channel}
                {" · to "}{m.recipient}
              </div>
            </div>
            <div className="shrink-0 flex flex-col items-end gap-1">
              <Pill tone={m.status === "sent" ? "green" : "amber"}>{m.status}</Pill>
              {m.template && (
                <span className="text-xs text-slate-400 font-mono">{m.template}</span>
              )}
            </div>
          </div>
          {m.body && (
            <pre className="mt-2 text-xs text-slate-700 whitespace-pre-wrap font-sans">
              {m.body}
            </pre>
          )}
        </Card>
      ))}
    </>
  );
}
