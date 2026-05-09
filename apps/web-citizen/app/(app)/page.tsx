"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  Card, EmptyState, Pill, SectionHeader, services, useSession,
} from "@naditos/web-common";

// Citizen home — a digital-wallet style summary that opens onto the
// six things a citizen actually owns / cares about. Fetches in
// parallel; each card renders even when the count hasn't arrived yet
// so the layout doesn't shift.

type Counts = {
  vehicles: number;
  fines_open: number;
  has_license: boolean;
  license_points: number;
  unread_notifications: number;
  pending_transfers: number;
};

export default function CitizenHome() {
  const { session } = useSession();
  const [c, setC] = useState<Partial<Counts>>({});

  useEffect(() => {
    if (!session) return;
    const opts = { token: session.accessToken, tenant: session.user.tenant };

    services.registry("/v1/citizens/me/vehicles", opts)
      .then((r: any) => setC((s) => ({ ...s, vehicles: (r.items ?? []).length })))
      .catch(() => {});
    services.fines("/v1/fines/mine", opts)
      .then((r: any) => setC((s) => ({
        ...s,
        fines_open: (r.items ?? []).filter((f: any) =>
          ["issued", "warned", "overdue", "escalated"].includes(f.status)).length,
      }))).catch(() => {});
    services.license("/v1/citizens/me/license", opts)
      .then((r: any) => setC((s) => ({
        ...s, has_license: true, license_points: r.license?.points ?? 0,
      })))
      .catch(() => setC((s) => ({ ...s, has_license: false })));
    services.notify("/v1/citizens/me/notifications", opts)
      .then((r: any) => setC((s) => ({ ...s, unread_notifications: (r.items ?? []).length })))
      .catch(() => {});
    services.registry("/v1/citizens/me/transfers", opts)
      .then((r: any) => setC((s) => ({
        ...s,
        pending_transfers: (r.items ?? []).filter((t: any) => t.status === "pending").length,
      })))
      .catch(() => {});
  }, [session]);

  return (
    <>
      <SectionHeader
        eyebrow={`Welcome, ${session?.user.full_name?.split(" ")[0] ?? ""}`}
        title="My transport account"
        description="Your digital vehicle wallet, license standing, and fines — all in one trusted view."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Tile
          href="/owner"
          eyebrow="Identity"
          title="My profile"
          description="Claim or update the owner record used for fine notices and reminders."
        />
        <Tile
          href="/vehicles"
          eyebrow="Registry"
          title="My vehicles"
          description="Vehicles registered to you with live insurance and inspection status."
          badge={typeof c.vehicles === "number" ? <Pill tone="ops">{c.vehicles}</Pill> : undefined}
        />
        <Tile
          href="/fines"
          eyebrow="Enforcement"
          title="My fines"
          description="View, dispute, or pay outstanding fines."
          badge={
            c.fines_open === undefined ? undefined :
            c.fines_open > 0
              ? <Pill tone="amber">{c.fines_open} open</Pill>
              : <Pill tone="green">none open</Pill>
          }
        />
        <Tile
          href="/license"
          eyebrow="Licensing"
          title="My driver license"
          description="License standing, demerit points, and suspension history."
          badge={
            !c.has_license ? undefined :
            <Pill tone={(c.license_points ?? 0) >= 9 ? "red" : (c.license_points ?? 0) >= 6 ? "amber" : "green"}>
              {c.license_points ?? 0} pts
            </Pill>
          }
        />
        <Tile
          href="/inbox"
          eyebrow="Communication"
          title="Notifications"
          description="Messages sent to your registered email or phone."
          badge={
            c.unread_notifications && c.unread_notifications > 0
              ? <Pill tone="ops">{c.unread_notifications}</Pill>
              : undefined
          }
        />
        <Tile
          href="/transfers"
          eyebrow="Ownership"
          title="Transfers"
          description="Hand a vehicle to a buyer, or accept one with a transfer code."
          badge={
            c.pending_transfers && c.pending_transfers > 0
              ? <Pill tone="amber">{c.pending_transfers} pending</Pill>
              : undefined
          }
        />
      </div>
    </>
  );
}

function Tile({
  href, eyebrow, title, description, badge,
}: {
  href: string;
  eyebrow: string;
  title: string;
  description: string;
  badge?: React.ReactNode;
}) {
  return (
    <Link href={href}
      className="group rounded-[var(--r-lg)] focus-visible:outline-none
                 focus-visible:[box-shadow:var(--focus-ring)] block">
      <Card pad="md" tone="default"
        className="group-hover:shadow-[var(--e-raised)] group-hover:ring-[var(--accent-primary)]/30
                   transition-[box-shadow] duration-[var(--m-base)] h-full">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">
              {eyebrow}
            </div>
            <div className="mt-0.5 text-lg font-semibold text-[var(--fg-primary)]"
                 style={{ fontFamily: "var(--ff-display)" }}>
              {title}
            </div>
            <div className="mt-1 text-[13px] text-[var(--fg-secondary)]">
              {description}
            </div>
          </div>
          <div className="shrink-0">{badge}</div>
        </div>
        <div className="mt-3 inline-flex items-center gap-1 text-[12px] uppercase tracking-[0.14em]
                        text-[var(--accent-soft-fg)] group-hover:text-[var(--accent-strong)]">
          Open <ArrowRight />
        </div>
      </Card>
    </Link>
  );
}

function ArrowRight() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
         strokeLinecap="round" strokeLinejoin="round" className="h-3.5 w-3.5">
      <path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>
    </svg>
  );
}
