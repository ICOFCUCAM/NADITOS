"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  Card, EmptyState, Pill, Plate, SectionHeader, Skeleton,
  services, useSession,
} from "@naditos/web-common";

type Fine = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string; issued_at: string;
};

const STATUS_TONE: Record<string, "green" | "amber" | "red" | "slate"> = {
  paid: "green",
  cancelled: "slate",
  disputed: "amber",
  overdue: "red",
  escalated: "red",
  issued: "amber",
  warned: "amber",
};

export default function RecentPage() {
  const { session } = useSession();
  const [items, setItems] = useState<Fine[] | null>(null);

  useEffect(() => {
    if (!session) return;
    services.fines("/v1/fines/issued-by-me", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setItems(r.items ?? []))
      .catch(() => setItems([]));
  }, [session]);

  const totals = useMemo(() => {
    if (!items) return null;
    const open = items.filter(f =>
      !["paid", "cancelled"].includes(f.status)
    ).length;
    const today = items.filter(f =>
      new Date(f.issued_at).toDateString() === new Date().toDateString()
    ).length;
    return { total: items.length, open, today };
  }, [items]);

  return (
    <div className="px-4 pt-4 space-y-4">
      <SectionHeader
        eyebrow="Officer log"
        title="Recent fines"
        description="Charges you've issued. The chain of custody is sealed
                     at issuance and tracks every handler from there."
      />

      {totals && (
        <div className="grid grid-cols-3 gap-2">
          <MiniStat label="Total" value={totals.total} />
          <MiniStat label="Open" value={totals.open} tone="warn" />
          <MiniStat label="Today" value={totals.today} tone="ops" />
        </div>
      )}

      {items === null && (
        <div className="space-y-3">
          {[0,1,2].map(i => <Skeleton key={i} className="h-20 w-full" />)}
        </div>
      )}

      {items?.length === 0 && (
        <Card pad="lg" tone="outline">
          <EmptyState
            title="No fines on record"
            description="Charges you issue will appear here in real time."
          />
        </Card>
      )}

      {items && items.length > 0 && (
        <div className="space-y-2.5">
          {items.map((f) => (
            <Link key={f.id} href={`/fine/${f.id}`}
                  className="block rounded-[var(--r-lg)] bg-[var(--bg-surface)]
                             ring-1 ring-[var(--border-subtle)] p-4
                             hover:ring-[var(--border-strong)]
                             focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)]
                             transition-[box-shadow]">
              <div className="flex items-center justify-between gap-3">
                <Plate value={f.plate} size="md" />
                <Pill tone={STATUS_TONE[f.status] ?? "slate"}>{f.status}</Pill>
              </div>
              <div className="mt-2 flex items-center justify-between text-sm">
                <span className="text-[var(--fg-secondary)]">{f.offence_code}</span>
                <span className="text-[var(--fg-primary)] font-semibold tabular-nums">
                  {f.amount} {f.currency}
                </span>
              </div>
              <div className="mt-1 text-[11px] uppercase tracking-[0.14em] text-[var(--fg-muted)]">
                {new Date(f.issued_at).toLocaleString()}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function MiniStat({
  label, value, tone,
}: { label: string; value: number; tone?: "ops" | "warn" }) {
  const v =
    tone === "ops" ? "text-[var(--accent-primary)]" :
    tone === "warn" ? "text-[var(--c-warn-300)]" :
                     "text-[var(--fg-primary)]";
  return (
    <div className="rounded-[var(--r-md)] bg-[var(--bg-surface)] ring-1 ring-[var(--border-subtle)] px-3 py-2">
      <div className="text-[10px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">{label}</div>
      <div className={`text-2xl font-semibold leading-none ${v}`}
           style={{ fontFamily: "var(--ff-display)" }}>{value}</div>
    </div>
  );
}
