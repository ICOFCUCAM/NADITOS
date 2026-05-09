"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { Card, Input, Pill, services, useSession } from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string;
  issued_at: string; due_at: string;
};

type Filter = "all" | "open" | "paid" | "disputed" | "cancelled";
const FILTERS: Filter[] = ["all", "open", "paid", "disputed", "cancelled"];
const FILTER_LABEL: Record<Filter, string> = {
  all:       "All",
  open:      "Open",
  paid:      "Paid",
  disputed:  "Disputed",
  cancelled: "Cancelled",
};

const OPEN_STATUSES = new Set(["issued", "warned", "overdue", "escalated"]);

export default function FinesPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[]>([]);
  const [filter, setFilter] = useState<Filter>("all");
  const [q, setQ] = useState("");

  useEffect(() => {
    if (!session) return;
    services.fines("/v1/fines", { token: session.accessToken, tenant: session.user.tenant })
      .then((r: any) => setItems(r.items ?? []))
      .catch(() => setItems([]));
  }, [session]);

  // Bucket counts come from the unfiltered set so the filter pills
  // remain a stable sense of "what's out there".
  const counts: Record<Filter, number> = useMemo(() => ({
    all:       items.length,
    open:      items.filter((f) => OPEN_STATUSES.has(f.status)).length,
    paid:      items.filter((f) => f.status === "paid").length,
    disputed:  items.filter((f) => f.status === "disputed").length,
    cancelled: items.filter((f) => f.status === "cancelled").length,
  }), [items]);

  const filtered = useMemo(() => {
    let out = items;
    if (filter === "open")      out = out.filter((f) => OPEN_STATUSES.has(f.status));
    else if (filter !== "all")  out = out.filter((f) => f.status === filter);
    if (q.trim()) {
      const needle = q.trim().toUpperCase();
      out = out.filter((f) => f.plate.includes(needle) || f.offence_code.includes(needle));
    }
    return out;
  }, [items, filter, q]);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Fines</h1>

      <div className="flex flex-wrap gap-2 items-center">
        {FILTERS.map((k) => (
          <button key={k} onClick={() => setFilter(k)}
            className={`px-3 py-1.5 rounded-full text-xs ring-1 transition ${
              filter === k
                ? "bg-slate-900 text-white ring-slate-900"
                : "bg-white text-slate-700 ring-slate-300 hover:bg-slate-50"
            }`}>
            {FILTER_LABEL[k]} <span className="opacity-60">({counts[k]})</span>
          </button>
        ))}
        <div className="flex-1 min-w-[200px]">
          <Input value={q} onChange={(e) => setQ(e.target.value.toUpperCase())}
            placeholder="Filter by plate or offence" />
        </div>
      </div>

      <Card className="p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-600">
            <tr>
              <th className="text-left p-3">Plate</th>
              <th className="text-left p-3">Offence</th>
              <th className="text-left p-3">Amount</th>
              <th className="text-left p-3">Status</th>
              <th className="text-left p-3">Issued</th>
              <th className="text-left p-3">Due</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((f) => (
              <tr key={f.id} className="border-t border-slate-100 hover:bg-slate-50">
                <td className="p-3 font-mono">
                  <Link href={`/fines/${f.id}`} className="hover:underline">
                    {f.plate}
                  </Link>
                </td>
                <td className="p-3">{f.offence_code}</td>
                <td className="p-3">{f.amount} {f.currency}</td>
                <td className="p-3">{tone(f.status)}</td>
                <td className="p-3">{fmt(f.issued_at)}</td>
                <td className="p-3">{fmt(f.due_at)}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr><td className="p-6 text-center text-slate-500" colSpan={6}>
                {items.length === 0 ? "No fines yet." : "No fines match the filter."}
              </td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

function tone(status: string) {
  switch (status) {
    case "paid":      return <Pill tone="green">paid</Pill>;
    case "cancelled": return <Pill>cancelled</Pill>;
    case "disputed":  return <Pill tone="amber">disputed</Pill>;
    case "escalated": return <Pill tone="red">escalated</Pill>;
    default:          return <Pill tone="amber">{status}</Pill>;
  }
}
function fmt(iso: string) { return new Date(iso).toLocaleString(); }
