"use client";

// Officer-side detail view for a fine they issued.

import Link from "next/link";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  Card, EmptyState, KeyValue, Mono, Pill, Plate, SectionHeader, Skeleton,
  services, useSession,
} from "@naditos/web-common";

type Evidence = {
  kind: string; s3_key: string; sha256: string; bytes: number; taken_at: string;
};
type Custody = {
  evidence_id: string; action: string;
  actor_role?: string | null; actor_device?: string | null; occurred_at: string;
};
type FineFields = {
  id: string; plate: string; offence_code: string;
  amount: string; currency: string; status: string;
  issued_at: string; due_at: string; escalation_stage: number;
};
type FineDetail = { fine: FineFields; evidence?: Evidence[]; custody?: Custody[]; };

const STAGE_LABEL: Record<number, string> = {
  1: "Stage 1 · warning", 2: "Stage 2 · penalty", 3: "Stage 3 · flag",
  4: "Stage 4 · seize",   5: "Stage 5 · court",
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

  if (err) return (
    <div className="px-4 pt-4">
      <Card pad="md" tone="alert">Couldn't load: {err}</Card>
    </div>
  );
  if (!data) return (
    <div className="px-4 pt-4 space-y-3">
      <Skeleton className="h-32 w-full" />
      <Skeleton className="h-24 w-full" />
    </div>
  );

  const { fine } = data;
  const evidence = data.evidence ?? [];
  const custody = data.custody ?? [];

  return (
    <div className="px-4 pt-4 space-y-4">
      <SectionHeader
        eyebrow={<>Fine · <Mono>{fine.id.slice(0,8)}</Mono></>}
        title={<Plate value={fine.plate} size="xl" />}
        actions={
          <Link href="/recent" className="text-sm text-[var(--fg-muted)] hover:text-[var(--fg-primary)]">
            ← Back
          </Link>
        }
      />

      <Card pad="md" tone="elevated">
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="text-[11px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">{fine.offence_code}</div>
            <div className="mt-1 text-3xl font-semibold"
                 style={{ fontFamily: "var(--ff-display)" }}>
              {fine.amount} <span className="text-[var(--fg-muted)] text-base">{fine.currency}</span>
            </div>
          </div>
          <Pill tone={fine.status === "paid" ? "green" : fine.status === "cancelled" ? "slate" : "amber"}>
            {fine.status}
          </Pill>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-3">
          <KeyValue k="Issued"  v={new Date(fine.issued_at).toLocaleString()} />
          <KeyValue k="Due"     v={new Date(fine.due_at).toLocaleDateString()} />
        </div>
        {fine.escalation_stage > 0 && (
          <div className="mt-3">
            <Pill tone="red">{STAGE_LABEL[fine.escalation_stage] ?? `Stage ${fine.escalation_stage}`}</Pill>
          </div>
        )}
      </Card>

      <SubHeader>Evidence ({evidence.length})</SubHeader>
      {evidence.length === 0 && (
        <Card pad="md" tone="alert">
          No evidence on file — anti-corruption gate should have blocked this.
        </Card>
      )}
      {evidence.map((e) => (
        <Card key={e.sha256} pad="sm" tone="elevated">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-sm font-medium">{e.kind}</div>
              <div className="text-[11px] uppercase tracking-[0.14em] text-[var(--fg-muted)] mt-0.5">
                {new Date(e.taken_at).toLocaleString()} · {Math.round(e.bytes / 1024)} KB
              </div>
              <div className="mt-1.5 text-xs break-all"><Mono>sha256:{e.sha256.slice(0,40)}…</Mono></div>
            </div>
            <Pill tone="ops">{e.kind}</Pill>
          </div>
        </Card>
      ))}

      <SubHeader>Chain of custody ({custody.length})</SubHeader>
      {custody.length === 0 && (
        <Card pad="md" tone="outline">
          <EmptyState title="No custody events recorded." />
        </Card>
      )}
      <div className="space-y-2">
        {custody.map((c, i) => (
          <Card key={i} pad="sm" tone="elevated">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-sm">
                  <Pill tone="ops">{c.action}</Pill>
                  {c.actor_role && (
                    <span className="ml-2 text-[var(--fg-secondary)]">
                      by <Mono>{c.actor_role}</Mono>
                    </span>
                  )}
                </div>
                <div className="mt-1 text-[11px] uppercase tracking-[0.14em] text-[var(--fg-muted)]">
                  {new Date(c.occurred_at).toLocaleString()}
                  {c.actor_device && <> · device <Mono>{c.actor_device}</Mono></>}
                </div>
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}

function SubHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mt-2">
      {children}
    </div>
  );
}
