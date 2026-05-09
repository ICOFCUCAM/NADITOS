"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  Card, Input, Pill, Plate, SectionHeader,
  services, useSession, statusLabel, type VehicleStatus,
} from "@naditos/web-common";

type Vehicle = {
  id: string; plate: string; make?: string; model?: string; year?: number;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
  is_seized: boolean;
  is_wanted: boolean;
};

const STATUS_TONE_CLS: Record<VehicleStatus, string> = {
  green:  "bg-[var(--status-ok-bg)]   text-[var(--status-ok-fg)]   ring-[var(--c-ok-500)]/40",
  yellow: "bg-[var(--status-warn-bg)] text-[var(--status-warn-fg)] ring-[var(--c-warn-500)]/40",
  red:    "bg-[var(--status-bad-bg)]  text-[var(--status-bad-fg)]  ring-[var(--c-bad-500)]/40",
  black:  "bg-black                   text-white                    ring-white/40",
};

export default function VehiclesPage() {
  const { session } = useSession();
  const [q, setQ] = useState("");
  const [flaggedOnly, setFlaggedOnly] = useState(false);
  const [items, setItems] = useState<Vehicle[]>([]);

  useEffect(() => {
    if (!session) return;
    const t = setTimeout(() => {
      const params = new URLSearchParams();
      if (q) params.set("q", q);
      if (flaggedOnly) params.set("flagged", "1");
      services.registry(`/v1/vehicles?${params.toString()}`, {
        token: session.accessToken, tenant: session.user.tenant,
      }).then((r: any) => setItems(r.items ?? [])).catch(() => setItems([]));
    }, 200);
    return () => clearTimeout(t);
  }, [q, flaggedOnly, session]);

  return (
    <div className="space-y-5">
      <SectionHeader
        eyebrow="Registry"
        title="Vehicles"
        description="Searchable jurisdiction registry. Toggle Flagged-only to triage operational alerts."
      />

      <div className="flex flex-wrap items-center gap-3">
        <Input placeholder="Search plate or VIN…" value={q}
          onChange={(e) => setQ(e.target.value)}
          className="flex-1 min-w-[16rem]" inputSize="lg" />
        <label className="inline-flex items-center gap-2 select-none rounded-[var(--r-md)]
                          bg-[var(--bg-elevated)] ring-1 ring-[var(--border-default)] px-3 py-2 text-sm">
          <input type="checkbox" checked={flaggedOnly}
            className="accent-[var(--accent-primary)]"
            onChange={(e) => setFlaggedOnly(e.target.checked)} />
          <span className="text-[var(--fg-secondary)]">Flagged only</span>
          <span className="text-[var(--fg-muted)] text-[11px] uppercase tracking-[0.10em]">
            stolen · seized · wanted
          </span>
        </label>
      </div>

      <Card pad="none" tone="elevated" className="overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-[var(--bg-hover)] text-[var(--fg-muted)]">
            <tr className="text-[11px] uppercase tracking-[0.14em]">
              <th className="text-left px-4 py-3 font-medium">Plate</th>
              <th className="text-left px-4 py-3 font-medium">Make/Model</th>
              <th className="text-left px-4 py-3 font-medium">Status</th>
              <th className="text-left px-4 py-3 font-medium">Flags</th>
              <th className="text-left px-4 py-3 font-medium">Insurance</th>
              <th className="text-left px-4 py-3 font-medium">Inspection</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id} className="border-t border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] transition-[background]">
                <td className="px-4 py-3">
                  <Link href={`/vehicles/${v.id}`}
                    className="focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] rounded-[var(--r-sm)]">
                    <Plate value={v.plate} size="sm" />
                  </Link>
                </td>
                <td className="px-4 py-3 text-[var(--fg-secondary)]">
                  {[v.make, v.model, v.year].filter(Boolean).join(" ") || <span className="text-[var(--fg-muted)]">—</span>}
                </td>
                <td className="px-4 py-3">
                  <span className={
                    "inline-flex items-center gap-1.5 rounded-[var(--r-pill)] px-2.5 py-0.5 " +
                    "text-[11px] font-medium uppercase tracking-[0.06em] ring-1 " +
                    STATUS_TONE_CLS[v.status]
                  }>
                    <span className="h-1.5 w-1.5 rounded-full" style={{
                      background:
                        v.status === "green"  ? "var(--c-ok-500)" :
                        v.status === "yellow" ? "var(--c-warn-500)" :
                        v.status === "red"    ? "var(--c-bad-500)" : "white",
                    }} />
                    {statusLabel[v.status]}
                  </span>
                </td>
                <td className="px-4 py-3 space-x-1">
                  {v.is_stolen && <Pill tone="black">stolen</Pill>}
                  {v.is_seized && <Pill tone="red">seized</Pill>}
                  {v.is_wanted && <Pill tone="amber">wanted</Pill>}
                  {!v.is_stolen && !v.is_seized && !v.is_wanted && (
                    <span className="text-[var(--fg-muted)]">—</span>
                  )}
                </td>
                <td className="px-4 py-3 text-[var(--fg-secondary)] tabular-nums">{fmt(v.insurance_expires_at)}</td>
                <td className="px-4 py-3 text-[var(--fg-secondary)] tabular-nums">{fmt(v.inspection_expires_at)}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td className="px-6 py-12 text-center text-[var(--fg-muted)]" colSpan={6}>
                {flaggedOnly ? "No flagged vehicles." : "No vehicles match."}
              </td></tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

function fmt(iso?: string | null) {
  if (!iso) return "—";
  return new Date(iso).toISOString().slice(0, 10);
}
