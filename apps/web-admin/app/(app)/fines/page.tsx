"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  Card, Input, Pill, Plate, SectionHeader, services, useSession,
} from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string;
  issued_at: string; due_at: string;
};

type Filter = "all" | "open" | "paid" | "disputed" | "cancelled";
const FILTERS: Filter[] = ["all", "open", "paid", "disputed", "cancelled"];
const FILTER_LABEL: Record<Filter, string> = {
  all: "All", open: "Open", paid: "Paid",
  disputed: "Disputed", cancelled: "Cancelled",
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

  const counts: Record<Filter, number> = useMemo(() => ({
    all:       items.length,
    open:      items.filter((f) => OPEN_STATUSES.has(f.status)).length,
    paid:      items.filter((f) => f.status === "paid").length,
    disputed:  items.filter((f) => f.status === "disputed").length,
    cancelled: items.filter((f) => f.status === "cancelled").length,
  }), [items]);

  const filtered = useMemo(() => {
    let out = items;
    if (filter === "open")     out = out.filter((f) => OPEN_STATUSES.has(f.status));
    else if (filter !== "all") out = out.filter((f) => f.status === filter);
    if (q.trim()) {
      const needle = q.trim().toUpperCase();
      out = out.filter((f) => f.plate.includes(needle) || f.offence_code.includes(needle));
    }
    return out;
  }, [items, filter, q]);

  return (
    <div className="space-y-5">
      <SectionHeader
        eyebrow="Enforcement"
        title="Fines"
        description="All fines for this jurisdiction. Click a row to open the
                     evidence and chain-of-custody view."
      />

      <div className="flex flex-wrap gap-2 items-center">
        {FILTERS.map((k) => (
          <button key={k} onClick={() => setFilter(k)}
            className={
              "px-3 py-1.5 rounded-[var(--r-pill)] text-[11px] uppercase " +
              "tracking-[0.10em] ring-1 transition-[background,color] " +
              "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
              (filter === k
                ? "bg-[var(--accent-primary)] text-[var(--accent-primary-fg)] ring-[var(--accent-primary)]"
                : "bg-[var(--bg-elevated)] text-[var(--fg-secondary)] ring-[var(--border-default)] hover:bg-[var(--bg-hover)]")
            }>
            {FILTER_LABEL[k]} <span className="opacity-60 tabular-nums">({counts[k]})</span>
          </button>
        ))}
        <div className="flex-1 min-w-[240px]">
          <Input value={q} onChange={(e) => setQ(e.target.value.toUpperCase())}
            placeholder="Filter by plate or offence" />
        </div>
      </div>

      <Card pad="none" tone="elevated" className="overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-[var(--bg-hover)] text-[var(--fg-muted)]">
            <tr className="text-[11px] uppercase tracking-[0.14em]">
              <th className="text-left px-4 py-3 font-medium">Plate</th>
              <th className="text-left px-4 py-3 font-medium">Offence</th>
              <th className="text-right px-4 py-3 font-medium">Amount</th>
              <th className="text-left px-4 py-3 font-medium">Status</th>
              <th className="text-left px-4 py-3 font-medium">Issued</th>
              <th className="text-left px-4 py-3 font-medium">Due</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((f) => (
              <tr key={f.id}
                  className="border-t border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] transition-[background]">
                <td className="px-4 py-3">
                  <Link href={`/fines/${f.id}`}
                    className="focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] rounded-[var(--r-sm)]">
                    <Plate value={f.plate} size="sm" />
                  </Link>
                </td>
                <td className="px-4 py-3 font-mono text-[12px] text-[var(--fg-secondary)]">{f.offence_code}</td>
                <td className="px-4 py-3 text-right tabular-nums font-medium">{f.amount} <span className="text-[var(--fg-muted)]">{f.currency}</span></td>
                <td className="px-4 py-3">{tone(f.status)}</td>
                <td className="px-4 py-3 text-[var(--fg-secondary)]">{fmt(f.issued_at)}</td>
                <td className="px-4 py-3 text-[var(--fg-secondary)]">{fmt(f.due_at)}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr><td className="px-6 py-12 text-center text-[var(--fg-muted)]" colSpan={6}>
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
