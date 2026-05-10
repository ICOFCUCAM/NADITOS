"use client";

import { useEffect, useState } from "react";
import {
  Card, Pill, SectionHeader, Skeleton, Stat,
  services, useSession,
} from "@naditos/web-common";

// Insurance partner overview. The insurance service exposes a
// per-tenant health endpoint (provider state, last success, fail
// streak); the listing endpoints for policies/claims aren't built yet
// so the bottom panels point at the integration docs instead.
//
//   GET /v1/insurance/health  — provider state for this tenant

type Health = {
  provider: string;
  module: string;
  state: string;       // "ok" | "down" | "degraded"
  fail_streak: number;
  last_ok_at?: string | null;
  last_fail_at?: string | null;
};

export default function InsuranceHome() {
  const { session } = useSession();
  const [health, setHealth] = useState<Health | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!session) return;
    setLoading(true);
    services.insurance(`/v1/insurance/health`, {
      token: session.accessToken, tenant: session.user.tenant,
    })
      .then((h) => setHealth(h as Health))
      .catch(() => setHealth(null))
      .finally(() => setLoading(false));
  }, [session]);

  const stateTone =
    health?.state === "ok"       ? "green" :
    health?.state === "degraded" ? "amber" :
    health?.state === "down"     ? "red"   : "slate";

  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Provider integration"
        title={`Welcome, ${session?.user.full_name ?? ""}.`}
        description="Push policy lifecycle events; review delivery health."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Stat label="Provider state"
              value={loading ? <Skeleton className="h-4 w-20 inline-block" /> : (
                <span className="inline-flex items-center gap-2">
                  <Pill tone={stateTone}>{health?.state ?? "—"}</Pill>
                </span>
              )} />
        <Stat label="Provider"
              value={loading ? <Skeleton className="h-4 w-20 inline-block" /> : (health?.provider ?? "—")}
              hint={health?.module} />
        <Stat label="Fail streak"
              value={loading ? <Skeleton className="h-4 w-12 inline-block" /> : (health?.fail_streak ?? 0)} />
        <Stat label="Last success"
              value={loading ? <Skeleton className="h-4 w-20 inline-block" /> :
                     (health?.last_ok_at ? timeAgo(health.last_ok_at) : "—")}
              hint={health?.last_fail_at ? `last fail ${timeAgo(health.last_fail_at)}` : undefined} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card>
          <SectionHeader eyebrow="Webhook health" title="Recent deliveries"
            description="Last 24 hours of provider-side requests." />
          <p className="text-sm text-[var(--fg-secondary)] mt-3">
            Per-delivery listing isn't exposed yet. The aggregate state
            above comes from <code>GET /v1/insurance/health</code>.
            Webhook receiver: <code>POST /v1/insurance/webhooks/{`{provider}`}</code>.
          </p>
          <div className="mt-3"><Pill tone="ops">aggregate-only</Pill></div>
        </Card>
        <Card>
          <SectionHeader eyebrow="Documentation" title="Quick links"
            description="Integration guide, sample payloads, sandbox." />
          <ul className="text-sm text-[var(--fg-secondary)] mt-3 space-y-2">
            <li><code>POST /v1/insurance/webhooks/{`{provider}`}</code> — push event</li>
            <li><code>GET&nbsp; /v1/insurance/verify?plate={`{plate}`}</code> — verify policy</li>
            <li><code>POST /v1/insurance/reconcile</code> — admin reconcile</li>
          </ul>
        </Card>
      </div>
    </div>
  );
}

function timeAgo(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso).getTime();
  const s = Math.max(0, (Date.now() - d) / 1000);
  if (s < 60)   return `${Math.floor(s)}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400)return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
