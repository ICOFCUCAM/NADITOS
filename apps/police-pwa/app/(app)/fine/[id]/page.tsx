"use client";

// Officer-side detail view for a fine they issued.
//
// The /v1/fines/{id} endpoint authorises officers who issued the fine
// to read it (matching by issued_by). They see status, evidence list,
// and the full chain of custody — useful when court asks for the
// receipt-of-handling on a specific charge.

import Link from "next/link";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { services, useSession, Pill } from "@naditos/web-common";

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
  actor_role?: string | null;
  actor_device?: string | null;
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

type FineDetail = {
  fine: FineFields;
  evidence?: Evidence[];
  custody?: Custody[];
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

  if (err) return <div className="p-4 text-red-400 text-sm">Couldn't load: {err}</div>;
  if (!data) return <div className="p-4 text-slate-400 text-sm">Loading…</div>;
  const fine = data.fine;
  const evidence = data.evidence ?? [];
  const custody = data.custody ?? [];

  return (
    <div className="p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-bold">Fine {fine.id.slice(0, 8)}</h1>
        <Link href="/recent" className="text-sm text-slate-400 hover:text-white">
          ← Recent
        </Link>
      </div>

      <div className="rounded-lg bg-slate-800 p-3 space-y-1">
        <div className="flex justify-between">
          <div className="font-mono text-lg">{fine.plate}</div>
          <Pill tone={fine.status === "paid" ? "green" : "amber"}>{fine.status}</Pill>
        </div>
        <div className="text-sm text-slate-300">{fine.offence_code}</div>
        <div className="text-base">{fine.amount} {fine.currency}</div>
        <div className="text-xs text-slate-400">
          Issued {new Date(fine.issued_at).toLocaleString()}
          {" · "}due {new Date(fine.due_at).toLocaleDateString()}
        </div>
        {fine.escalation_stage > 0 && (
          <div className="text-xs text-amber-400">
            Escalation stage {fine.escalation_stage}
          </div>
        )}
      </div>

      {evidence.length > 0 && (
        <>
          <h2 className="text-xs uppercase tracking-wide text-slate-400 mt-3">Evidence</h2>
          {evidence.map((e) => (
            <div key={e.sha256} className="rounded bg-slate-800 p-2 text-xs space-y-1">
              <div className="font-medium">{e.kind}</div>
              <div className="text-slate-400">
                {new Date(e.taken_at).toLocaleString()}
                {" · "}{Math.round(e.bytes / 1024)} KB
              </div>
              <div className="font-mono text-slate-500 break-all">
                sha256: {e.sha256.slice(0, 32)}…
              </div>
            </div>
          ))}
        </>
      )}

      {custody.length > 0 && (
        <>
          <h2 className="text-xs uppercase tracking-wide text-slate-400 mt-3">Chain of custody</h2>
          {custody.map((c, i) => (
            <div key={i} className="rounded bg-slate-800 p-2 text-xs">
              <div>
                <span className="font-medium">{c.action}</span>{" "}
                {c.actor_role && <span className="text-slate-400">by {c.actor_role}</span>}
              </div>
              <div className="text-slate-400">
                {new Date(c.occurred_at).toLocaleString()}
                {c.actor_device && ` · device ${c.actor_device}`}
              </div>
            </div>
          ))}
        </>
      )}
    </div>
  );
}
