"use client";

// Admin dispute review.
//
// Citizens file disputes via /v1/fines/{id}/dispute. The fine status
// flips to "disputed" until an admin resolves it. This page lists all
// pending disputes with the parent fine context (plate, offence,
// amount) + reason text, and lets the admin resolve with one of:
//
//   accepted  — citizen wins; fine cancelled
//   rejected  — admin upholds the fine; status returns to issued
//   court     — escalates out of administrative remedy
//
// Each resolve writes an audit event with the prior status, the
// outcome, the admin's note, and the new fine status.

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Button, Card, Pill, services, useSession } from "@naditos/web-common";

type Dispute = {
  id: string;
  fine_id: string;
  plate: string;
  offence_code: string;
  amount: string;
  currency: string;
  filed_by: string;
  reason: string;
  status: string;
  filed_at: string;
};

type Outcome = "accepted" | "rejected" | "court";

const STATUS_FILTERS = ["pending", "accepted", "rejected", "court"] as const;
type StatusFilter = typeof STATUS_FILTERS[number];

export default function DisputesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Dispute[] | null>(null);
  const [filter, setFilter] = useState<StatusFilter>("pending");
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!session) return;
    try {
      const r: any = await services.fines(`/v1/fines/disputes?status=${filter}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setItems(r.items ?? []);
      setErr(null);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to load");
    }
  }, [session, filter]);
  useEffect(() => { refresh(); }, [refresh]);

  async function resolve(d: Dispute, outcome: Outcome) {
    if (!session) return;
    const note = window.prompt(
      `Resolve as ${outcome.toUpperCase()} — note (recorded in audit):`) ?? "";
    if (note === "") return; // cancelled
    setBusy(d.id);
    try {
      await services.fines(
        `/v1/fines/${d.fine_id}/disputes/${d.id}/resolve`, {
        method: "POST", body: { outcome, note },
        token: session.accessToken, tenant: session.user.tenant,
      });
      await refresh();
    } catch (e: any) {
      setErr(e?.message ?? "Resolve failed");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Fine disputes</h1>
        <p className="text-sm text-slate-600">
          Citizens file disputes from their portal; admins resolve here.
          Every resolution stamps the audit chain with prior status,
          outcome, and the admin's note.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        {STATUS_FILTERS.map((s) => (
          <button key={s} onClick={() => setFilter(s)}
            className={`px-3 py-1.5 rounded-full text-xs ring-1 transition ${
              filter === s
                ? "bg-slate-900 text-white ring-slate-900"
                : "bg-white text-slate-700 ring-slate-300 hover:bg-slate-50"
            }`}>
            {s}
          </button>
        ))}
      </div>

      {err && <Card className="text-red-700">{err}</Card>}

      {items === null && <Card>Loading…</Card>}
      {items !== null && items.length === 0 && (
        <Card>
          <p className="text-sm text-slate-500">
            No {filter} disputes.
          </p>
        </Card>
      )}

      {items?.map((d) => (
        <Card key={d.id}>
          <div className="flex items-start justify-between gap-3">
            <div className="space-y-1 min-w-0">
              <div className="flex items-center gap-2">
                <Link href={`/fines/${d.fine_id}`} className="font-mono text-lg hover:underline">
                  {d.plate}
                </Link>
                <span className="text-sm text-slate-600">{d.offence_code}</span>
                <span className="text-sm font-medium">{d.amount} {d.currency}</span>
              </div>
              <div className="text-xs text-slate-500">
                Filed {new Date(d.filed_at).toLocaleString()} · by{" "}
                <span className="font-mono">{d.filed_by.slice(0, 8)}</span>
              </div>
              <div className="mt-2 text-sm bg-slate-50 rounded p-2 italic">
                "{d.reason}"
              </div>
            </div>
            <div className="shrink-0 flex flex-col items-end gap-2">
              <Pill tone={d.status === "pending" ? "amber" :
                          d.status === "accepted" ? "green" :
                          d.status === "rejected" ? "red" : "slate"}>
                {d.status}
              </Pill>
            </div>
          </div>

          {d.status === "pending" && (
            <div className="mt-3 pt-3 border-t border-slate-100 flex flex-wrap gap-2 justify-end">
              <Button
                onClick={() => resolve(d, "accepted")}
                disabled={busy === d.id}
                className="bg-emerald-600 hover:bg-emerald-700">
                Accept (cancel fine)
              </Button>
              <Button
                onClick={() => resolve(d, "rejected")}
                disabled={busy === d.id}
                className="bg-red-600 hover:bg-red-700">
                Reject (uphold)
              </Button>
              <Button
                onClick={() => resolve(d, "court")}
                disabled={busy === d.id}
                className="bg-slate-700 hover:bg-slate-800">
                Escalate to court
              </Button>
            </div>
          )}
        </Card>
      ))}
    </div>
  );
}
