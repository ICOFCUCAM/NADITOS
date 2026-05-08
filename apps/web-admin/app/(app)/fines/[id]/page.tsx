"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { Card, Pill, services, useSession } from "@naditos/web-common";

type Evidence = {
  kind: string;
  s3_key: string;
  sha256: string;
  bytes: number;
  taken_at: string;
};

type FineDetail = {
  fine: {
    id: string; plate: string; offence_code: string;
    amount: string; currency: string; status: string;
    issued_at: string; due_at: string; issued_by: string;
  };
  evidence: Evidence[];
};

export default function FineDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { session } = useSession();
  const [data, setData] = useState<FineDetail | null>(null);

  useEffect(() => {
    if (!session || !id) return;
    services.fines(`/v1/fines/${id}`, {
      token: session.accessToken, tenant: session.user.tenant,
    }).then((r: any) => setData(r as FineDetail));
  }, [session, id]);

  if (!data) return <div className="text-slate-500">Loading…</div>;

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Fine {data.fine.id.slice(0, 8)}…</h1>

      <Card>
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>Plate: <span className="font-mono">{data.fine.plate}</span></div>
          <div>Offence: <span className="font-mono">{data.fine.offence_code}</span></div>
          <div>Amount: {data.fine.amount} {data.fine.currency}</div>
          <div>Status: <Pill tone={data.fine.status === "paid" ? "green" : "amber"}>{data.fine.status}</Pill></div>
          <div>Issued: {new Date(data.fine.issued_at).toLocaleString()}</div>
          <div>Due: {new Date(data.fine.due_at).toLocaleString()}</div>
          <div className="col-span-2 text-xs text-slate-500">Officer: <span className="font-mono">{data.fine.issued_by}</span></div>
        </div>
      </Card>

      <Card className="p-0 overflow-hidden">
        <div className="bg-slate-50 px-4 py-2 text-sm font-medium">Evidence ({data.evidence.length})</div>
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Kind</th>
              <th className="text-left p-3">Captured</th>
              <th className="text-left p-3">Bytes</th>
              <th className="text-left p-3">SHA-256 (verify with stored bytes)</th>
            </tr>
          </thead>
          <tbody>
            {data.evidence.map((e, i) => (
              <tr key={i} className="border-t border-slate-100">
                <td className="p-3"><Pill>{e.kind}</Pill></td>
                <td className="p-3">{new Date(e.taken_at).toLocaleString()}</td>
                <td className="p-3">{e.bytes.toLocaleString()}</td>
                <td className="p-3 font-mono text-xs break-all">{e.sha256}</td>
              </tr>
            ))}
            {data.evidence.length === 0 && (
              <tr><td className="p-6 text-center text-red-600" colSpan={4}>
                No evidence — this should be impossible (anti-corruption gate).
              </td></tr>
            )}
          </tbody>
        </table>
      </Card>

      <p className="text-xs text-slate-500">
        Object storage URLs (s3_key) are intentionally not rendered as preview
        images on this view; the secure evidence-viewer endpoint signs short-lived
        URLs after a chain-of-custody check. Phase-3 wires the preview iframe.
      </p>
    </div>
  );
}
