"use client";

// Single fine detail — citizen view.
//
// Shows the full record the citizen is entitled to see for due-process
// purposes: status, amount, escalation stage, the evidence list (kind,
// SHA-256, bytes, taken_at), and the chain-of-custody timeline that
// proves who handled the evidence and when.
//
// The s3_key is shown but not turned into a viewable URL — Phase-2
// will add a presigned-get endpoint scoped to the citizen's session.

import Link from "next/link";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { Card, Pill, services, useSession, Button } from "@naditos/web-common";

type Evidence = {
  kind: string;
  s3_key: string;
  sha256: string;
  bytes: number;
  taken_at: string;
};

type Custody = {
  evidence_id: string;
  action: string;
  actor_user?: string | null;
  actor_role?: string | null;
  actor_device?: string | null;
  details?: any;
  occurred_at: string;
};

type FineFields = {
  id: string;
  plate: string;
  offence_code: string;
  amount: string;
  currency: string;
  status: string;
  issued_at: string;
  due_at: string;
  escalation_stage: number;
};

// Backend returns the fine object nested under "fine" with parallel
// evidence and custody arrays — see services/fines/internal/api/api.go's
// get handler.
type FineDetail = {
  fine: FineFields;
  evidence?: Evidence[];
  custody?: Custody[];
};

const STAGE_LABEL: Record<number, string> = {
  1: "Stage 1 · warning",
  2: "Stage 2 · penalty",
  3: "Stage 3 · flag",
  4: "Stage 4 · seize",
  5: "Stage 5 · court",
};

export default function FineDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { session } = useSession();
  const [data, setData] = useState<FineDetail | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!session || !id) return;
    services.fines(`/v1/fines/${id}`, {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setData(r as FineDetail))
      .catch((e: any) => setErr(e?.message ?? "Failed to load"));
  }, [session, id]);

  if (err) return <Card className="text-red-700">Couldn't load: {err}</Card>;
  if (!data) return <Card>Loading…</Card>;
  const fine = data.fine;
  const evidence = data.evidence ?? [];
  const custody = data.custody ?? [];

  return (
    <>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Fine {fine.id.slice(0, 8)}</h1>
        <Link href="/fines" className="text-sm text-slate-600 hover:underline">
          ← Back
        </Link>
      </div>

      <Card>
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <div className="font-mono text-xl">{fine.plate}</div>
            <div className="text-sm text-slate-600">{fine.offence_code}</div>
            <div className="text-xl font-semibold">
              {fine.amount} {fine.currency}
            </div>
            <div className="text-xs text-slate-500">
              Issued {new Date(fine.issued_at).toLocaleDateString()} · Due{" "}
              {new Date(fine.due_at).toLocaleDateString()}
            </div>
            {fine.escalation_stage > 0 && (
              <div className="text-xs text-amber-700">
                {STAGE_LABEL[fine.escalation_stage] ?? `Stage ${fine.escalation_stage}`}
              </div>
            )}
          </div>
          <Pill tone={fine.status === "paid" ? "green" : "amber"}>{fine.status}</Pill>
        </div>
      </Card>

      <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-2">
        Evidence
      </h2>
      {evidence.length === 0 && (
        <Card className="text-sm text-slate-500">No evidence on file.</Card>
      )}
      {evidence.map((e) => (
        <Card key={e.sha256}>
          <div className="space-y-1 text-sm">
            <div className="font-medium">{e.kind}</div>
            <div className="text-xs text-slate-500">
              Captured {new Date(e.taken_at).toLocaleString()} ·{" "}
              {Math.round(e.bytes / 1024)} KB
            </div>
            <div className="font-mono text-xs text-slate-600 break-all">
              SHA-256: {e.sha256.slice(0, 32)}…
            </div>
            <div className="font-mono text-xs text-slate-600 break-all">
              s3://{e.s3_key}
            </div>
          </div>
        </Card>
      ))}

      <h2 className="text-sm uppercase tracking-wide text-slate-500 mt-2">
        Chain of custody
      </h2>
      {custody.length === 0 && (
        <Card className="text-sm text-slate-500">No custody events.</Card>
      )}
      {custody.map((c, i) => (
        <Card key={i}>
          <div className="text-sm">
            <span className="font-medium">{c.action}</span>{" "}
            {c.actor_role && (
              <span className="text-slate-500">by {c.actor_role}</span>
            )}
          </div>
          <div className="text-xs text-slate-500">
            {new Date(c.occurred_at).toLocaleString()}
            {c.actor_device && ` · device ${c.actor_device}`}
          </div>
        </Card>
      ))}
    </>
  );
}
