"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  Button, Card, Field, Input, Pill, Plate, SectionHeader,
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

      <NewVehicleForm onCreated={() => setQ((s) => s)} />

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

// NewVehicleForm — minimal create surface that respects the
// jurisdiction's plate format. The regex ships down with the session
// (user.tenant_config.plate_regex) so we can mirror the registry's
// server-side validation here and reject mismatches before the
// network call. If the regex isn't present (older backend, or the
// tenant row was missing fields), we accept anything client-side and
// let the server adjudicate.
function NewVehicleForm({ onCreated }: { onCreated: () => void }) {
  const { session } = useSession();
  const [open, setOpen] = useState(false);
  const [plate, setPlate] = useState("");
  const [make, setMake] = useState("");
  const [model, setModel] = useState("");
  const [year, setYear] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const regex = session?.user.tenant_config?.plate_regex;
  const compiled = useMemo(() => {
    if (!regex) return null;
    try { return new RegExp(regex); } catch { return null; }
  }, [regex]);

  const trimmed = plate.trim();
  const localOk = !compiled || trimmed === "" || compiled.test(trimmed);

  if (!open) {
    return (
      <Card pad="md" tone="elevated" className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <div className="text-[11px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">
            Create
          </div>
          <div className="text-sm text-[var(--fg-primary)]">Register a new vehicle</div>
          {regex && (
            <div className="text-xs text-[var(--fg-muted)]">
              Plate format for <strong>{session?.user.tenant_config?.country_code ?? session?.user.tenant}</strong>:
              {" "}<code className="text-[var(--fg-secondary)]">{regex}</code>
            </div>
          )}
        </div>
        <Button tone="primary" onClick={() => { setOpen(true); setErr(null); }}>
          New vehicle
        </Button>
      </Card>
    );
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!session) return;
    if (!trimmed) { setErr("Plate is required."); return; }
    if (compiled && !compiled.test(trimmed)) {
      setErr(`Plate does not match jurisdiction format (${regex}).`);
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      const body: Record<string, unknown> = { plate: trimmed };
      if (make)  body.make = make;
      if (model) body.model = model;
      if (year)  body.year = Number(year);
      await services.registry("/v1/vehicles", {
        method: "POST", body,
        token: session.accessToken, tenant: session.user.tenant,
      });
      setPlate(""); setMake(""); setModel(""); setYear("");
      setOpen(false);
      onCreated();
    } catch (e: any) {
      setErr(e?.message ?? "Could not create vehicle.");
    } finally { setBusy(false); }
  }

  return (
    <Card pad="md" tone="elevated">
      <form onSubmit={submit} className="space-y-3">
        <div className="flex items-baseline justify-between">
          <div className="text-sm font-medium text-[var(--fg-primary)]">Register a new vehicle</div>
          <button type="button" onClick={() => { setOpen(false); setErr(null); }}
            className="text-xs text-[var(--fg-muted)] hover:text-[var(--fg-primary)]">
            Cancel
          </button>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          <Field label="Plate" error={!localOk ? `Format: ${regex}` : err && err.includes("plate") ? err : undefined}>
            <Input value={plate}
              onChange={(e) => setPlate(e.target.value.toUpperCase())}
              placeholder={regex ? exampleFromRegex(regex) : "ABC-123"}
              invalid={!localOk}
              autoFocus required />
          </Field>
          <Field label="Make">
            <Input value={make} onChange={(e) => setMake(e.target.value)} placeholder="e.g. Toyota" />
          </Field>
          <Field label="Model">
            <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="e.g. Hilux" />
          </Field>
          <Field label="Year">
            <Input value={year} onChange={(e) => setYear(e.target.value.replace(/\D/g, ""))}
              inputMode="numeric" maxLength={4} placeholder={String(new Date().getFullYear())} />
          </Field>
        </div>

        {regex && (
          <div className="text-xs text-[var(--fg-muted)]">
            Format hint: <code className="text-[var(--fg-secondary)]">{regex}</code>
          </div>
        )}
        {err && !err.toLowerCase().includes("plate") && (
          <div className="text-xs text-[var(--status-bad-fg)]">{err}</div>
        )}

        <div className="flex gap-2 pt-1">
          <Button type="submit" tone="primary" disabled={busy || !trimmed || !localOk}>
            {busy ? "Saving…" : "Create vehicle"}
          </Button>
        </div>
      </form>
    </Card>
  );
}

// exampleFromRegex turns a permissive regex into a passable placeholder
// value for the input field. It only handles the common shape we use
// (^[A-Z0-9-]{2,N}$ and friends) — anything fancier just falls back
// to a generic "ABC-123".
function exampleFromRegex(re: string): string {
  const m = re.match(/\{(\d+),(\d+)\}/);
  const n = m ? Math.min(Number(m[2]), 8) : 7;
  return ("ABC-12345").slice(0, Math.max(n, 4));
}
