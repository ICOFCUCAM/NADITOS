"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  Card, Pill, SectionHeader, Skeleton, Stat, StatusDot,
  services, useSession,
} from "@naditos/web-common";

// Ministry command center.
//
// One page that answers: "right now, what does this jurisdiction
// look like?" Top stats are sized for a wall display; the section
// below splits compliance, enforcement, and ops health into named
// columns so a duty officer can scan in seconds.

type Counts = {
  vehicles_total: number;
  vehicles_red: number;
  vehicles_black: number;
  fines_unpaid: number;
  fines_today: number;
  alerts_open: number;
};

export default function Dashboard() {
  const { session } = useSession();
  const [counts, setCounts] = useState<Counts | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!session) return;
    setLoading(true);
    Promise.all([
      services.registry("/v1/vehicles", { token: session.accessToken, tenant: session.user.tenant }).catch(() => null),
      services.fines("/v1/fines", { token: session.accessToken, tenant: session.user.tenant }).catch(() => null),
      services.audit("/v1/audit/alerts", { token: session.accessToken, tenant: session.user.tenant }).catch(() => null),
    ]).then(([v, f, a]: any) => {
      const items = v?.items ?? [];
      const fines = f?.items ?? [];
      const alerts = a?.items ?? [];
      const today = new Date().toISOString().slice(0, 10);
      setCounts({
        vehicles_total: items.length,
        vehicles_red:   items.filter((x: any) => x.status === "red").length,
        vehicles_black: items.filter((x: any) => x.status === "black").length,
        fines_unpaid:   fines.filter((x: any) => x.status !== "paid" && x.status !== "cancelled").length,
        fines_today:    fines.filter((x: any) => x.issued_at?.startsWith(today)).length,
        alerts_open:    alerts.length,
      });
    }).finally(() => setLoading(false));
  }, [session]);

  return (
    <div className="space-y-7">
      <SectionHeader
        eyebrow="Live snapshot"
        title="Command center"
        description="Real-time operational state across the National Transport Intelligence Platform."
        actions={
          <Pill tone="ops"><StatusDot tone="ops" pulse /> Streaming</Pill>
        }
      />

      {/* Headline stats — wall-display sized */}
      <div className="grid grid-cols-2 lg:grid-cols-6 gap-4">
        <BigStat label="Vehicles" value={counts?.vehicles_total} loading={loading} />
        <BigStat label="Non-compliant" value={counts?.vehicles_red} loading={loading} tone="warn" />
        <BigStat label="Alert list" value={counts?.vehicles_black} loading={loading} tone="bad" />
        <BigStat label="Outstanding fines" value={counts?.fines_unpaid} loading={loading} tone="warn" />
        <BigStat label="Fines today" value={counts?.fines_today} loading={loading} tone="ops" />
        <BigStat label="Open alerts" value={counts?.alerts_open} loading={loading}
                 tone={counts && counts.alerts_open > 0 ? "bad" : "muted"}
                 href="/audit" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        <Panel title="Compliance">
          <KpiRow label="Total registered" v={counts?.vehicles_total} />
          <KpiRow label="Red — non-compliant" v={counts?.vehicles_red} tone="warn" />
          <KpiRow label="Black — alert list" v={counts?.vehicles_black} tone="bad" />
          <FooterLink href="/vehicles?flagged=1">Open flagged registry →</FooterLink>
        </Panel>

        <Panel title="Enforcement">
          <KpiRow label="Fines outstanding" v={counts?.fines_unpaid} tone="warn" />
          <KpiRow label="Fines issued today" v={counts?.fines_today} tone="ops" />
          <KpiRow label="Open audit alerts" v={counts?.alerts_open}
                  tone={counts && counts.alerts_open > 0 ? "bad" : "muted"} />
          <FooterLink href="/disputes">Review disputes →</FooterLink>
        </Panel>

        <Panel title="Modules">
          <div className="flex flex-wrap gap-1.5">
            {[
              "Registry","Licenses","Fines","Audit","Insurance",
              "Inspection","ANPR","Notifications",
            ].map((m) => <Pill key={m} tone="green">{m}</Pill>)}
          </div>
          <div className="mt-4 text-[11px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">
            Health
          </div>
          <FooterLink href="/providers">Provider health board →</FooterLink>
        </Panel>
      </div>
    </div>
  );
}

function BigStat({
  label, value, tone = "default", loading, href,
}: {
  label: string;
  value: number | undefined;
  tone?: "default" | "ops" | "warn" | "bad" | "muted";
  loading?: boolean;
  href?: string;
}) {
  const display = loading ? null : (typeof value === "number" ? value : "—");
  const valueCls =
    tone === "ops"  ? "text-[var(--accent-primary)]" :
    tone === "warn" ? "text-[var(--c-warn-300)]" :
    tone === "bad"  ? "text-[var(--c-bad-300)]" :
    tone === "muted"? "text-[var(--fg-muted)]" :
                      "text-[var(--fg-primary)]";
  const inner = (
    <Card pad="md" tone="elevated"
      className={
        "h-full transition-[box-shadow] " +
        (href ? "hover:shadow-[var(--glow-ops)] cursor-pointer" : "")
      }>
      <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">{label}</div>
      <div className="mt-2 flex items-baseline gap-2">
        <div className={`text-4xl font-semibold leading-none tabular-nums ${valueCls}`}
             style={{ fontFamily: "var(--ff-display)" }}>
          {display === null ? <Skeleton className="h-8 w-16 inline-block" /> : display}
        </div>
      </div>
    </Card>
  );
  return href ? <Link href={href}>{inner}</Link> : inner;
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card pad="md" tone="elevated">
      <div className="flex items-center justify-between mb-3">
        <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">{title}</div>
      </div>
      <div className="space-y-2">{children}</div>
    </Card>
  );
}

function KpiRow({
  label, v, tone = "default",
}: {
  label: string;
  v: number | undefined;
  tone?: "default" | "ops" | "warn" | "bad" | "muted";
}) {
  const cls =
    tone === "ops"  ? "text-[var(--accent-primary)]" :
    tone === "warn" ? "text-[var(--c-warn-300)]" :
    tone === "bad"  ? "text-[var(--c-bad-300)]" :
    tone === "muted"? "text-[var(--fg-muted)]" :
                      "text-[var(--fg-primary)]";
  return (
    <div className="flex items-center justify-between">
      <div className="text-sm text-[var(--fg-secondary)]">{label}</div>
      <div className={`text-lg font-semibold tabular-nums ${cls}`}>
        {typeof v === "number" ? v : "—"}
      </div>
    </div>
  );
}

function FooterLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <Link href={href} className="
      mt-4 inline-flex items-center gap-1 text-[12px] uppercase tracking-[0.14em]
      text-[var(--accent-soft-fg)] hover:text-[var(--accent-strong)]
      focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] rounded-[var(--r-xs)]
    ">{children}</Link>
  );
}
