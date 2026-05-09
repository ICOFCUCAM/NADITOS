"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Button, Card, EmptyState, Pill, SectionHeader, Skeleton, Stat,
  services, useSession,
} from "@naditos/web-common";

// Officer self-view.
//
// Surface their identity (name, role, jurisdiction), 14-day issuance
// rollup, daily breakdown with anomaly z-score, and sign-out. The
// anomaly score is shown only to the officer themselves — admins see
// the cross-officer comparative view.

type Day = {
  day: string;
  fines_issued: number;
  fines_cancelled: number;
  fines_total: string;
  unique_plates: number;
  anomaly_score: number | null;
};

export default function MePage() {
  const { session, logout } = useSession();
  const router = useRouter();
  const [days, setDays] = useState<Day[] | null>(null);

  useEffect(() => {
    if (!session) return;
    services.audit("/v1/audit/officers/me/stats", {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((r: any) => setDays(r.items ?? []))
      .catch(() => setDays([]));
  }, [session]);

  const totals = (days ?? []).reduce(
    (acc, d) => ({
      issued: acc.issued + d.fines_issued,
      cancelled: acc.cancelled + d.fines_cancelled,
      plates: acc.plates + d.unique_plates,
    }),
    { issued: 0, cancelled: 0, plates: 0 },
  );

  return (
    <div className="px-4 pt-4 space-y-5">
      {/* Officer card */}
      <Card pad="md" tone="elevated">
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">Officer</div>
            <div className="text-xl font-semibold mt-0.5"
                 style={{ fontFamily: "var(--ff-display)" }}>
              {session?.user.full_name}
            </div>
            <div className="mt-1 text-[13px] text-[var(--fg-secondary)]">
              {session?.user.email}
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Pill tone="ops">{session?.user.role}</Pill>
              <Pill tone="slate">{session?.user.tenant}</Pill>
            </div>
          </div>
          <Button tone="ghost" size="sm"
            onClick={() => logout().then(() => router.replace("/login"))}>
            End shift
          </Button>
        </div>
      </Card>

      <SectionHeader
        eyebrow="Last 14 days"
        title="Activity"
        description="Computed hourly from the audit ledger." />

      <div className="grid grid-cols-3 gap-3">
        <Stat label="Issued" value={totals.issued} />
        <Stat label="Cancelled" value={totals.cancelled}
          delta={{
            value: totals.issued ? `${((totals.cancelled/totals.issued)*100).toFixed(0)}%` : "—",
            tone: "muted",
          }}
        />
        <Stat label="Unique plates" value={totals.plates} />
      </div>

      <div>
        <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-2">
          Daily breakdown
        </div>
        {days === null && (
          <div className="space-y-2">
            {[0,1,2].map(i => <Skeleton key={i} className="h-14 w-full" />)}
          </div>
        )}
        {days?.length === 0 && (
          <Card pad="md" tone="outline">
            <EmptyState
              title="No activity in the last 14 days"
              description="Fines you issue from the scan workspace will appear here."
            />
          </Card>
        )}
        {days && days.length > 0 && (
          <div className="space-y-2">
            {days.map((d) => (
              <DailyRow key={d.day} d={d} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function DailyRow({ d }: { d: Day }) {
  const z = d.anomaly_score;
  const tone =
    z === null ? "slate" :
    Math.abs(z) >= 3 ? "red" :
    Math.abs(z) >= 1.5 ? "amber" : "green";
  const sign = z !== null ? (z >= 0 ? "+" : "") : "";
  return (
    <div className="rounded-[var(--r-md)] bg-[var(--bg-surface)]
                    ring-1 ring-[var(--border-subtle)] px-3 py-2.5
                    flex items-center justify-between">
      <div>
        <div className="text-sm font-medium">{new Date(d.day).toLocaleDateString()}</div>
        <div className="text-[11px] tracking-[0.04em] text-[var(--fg-muted)]">
          {d.fines_issued} issued · {d.fines_cancelled} cancelled · {d.unique_plates} plates
        </div>
      </div>
      <Pill tone={tone as any}>
        {z === null ? "no baseline" : `${sign}${z.toFixed(1)}σ`}
      </Pill>
    </div>
  );
}
