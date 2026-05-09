"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  BottomTab, MobileShell, useSession, IconButton, StatusDot,
} from "@naditos/web-common";

// Officer-app shell.
//
// Mobile-first: full-bleed scan workspace, sticky header carrying
// the officer identity + jurisdiction + connection light, and a
// fixed bottom tab bar. Tabs are ≥56px tall so they stay reliable
// in gloves and at jog. The active tab gets an underbar accent so
// it's identifiable in peripheral vision while the officer is
// looking at a vehicle, not the screen.

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, loading } = useSession();
  const router = useRouter();
  const pathname = usePathname() || "";

  useEffect(() => {
    if (!loading && !session) router.replace("/login");
  }, [loading, session, router]);

  if (loading || !session) {
    return (
      <div className="min-h-screen grid place-items-center text-[var(--fg-muted)] text-sm">
        Authenticating…
      </div>
    );
  }

  const isActive = (href: string) =>
    pathname === href || pathname.startsWith(href + "/");

  return (
    <MobileShell
      topbar={
        <>
          <div className="flex items-center gap-3 min-w-0">
            <span
              aria-hidden
              className="h-8 w-8 rounded-[var(--r-sm)]
                         bg-[var(--accent-primary)] text-[var(--accent-primary-fg)]
                         grid place-items-center font-bold text-[13px]"
              style={{ fontFamily: "var(--ff-display)" }}
            >
              N
            </span>
            <div className="leading-tight min-w-0">
              <div className="text-[13px] font-semibold tracking-[0.04em] truncate"
                   style={{ fontFamily: "var(--ff-display)" }}>
                {session.user.full_name}
              </div>
              <div className="text-[10px] uppercase tracking-[0.16em] text-[var(--fg-muted)] truncate">
                {session.user.role} · {session.user.tenant}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span className="hidden sm:inline-flex items-center gap-1.5
                             text-[11px] uppercase tracking-[0.14em]
                             text-[var(--fg-muted)]">
              <StatusDot tone="green" pulse /> On shift
            </span>
            <IconButton label="Officer profile"
              onClick={() => router.push("/me")}>
              <ProfileIcon />
            </IconButton>
          </div>
        </>
      }
      bottom={
        <div className="grid grid-cols-4">
          <BottomTab href="/scan"   active={isActive("/scan")}   label="Scan"    icon={<ScanIcon />} />
          <BottomTab href="/verify" active={isActive("/verify")} label="Verify"  icon={<VerifyIcon />} />
          <BottomTab href="/recent" active={isActive("/recent")} label="Recent"  icon={<RecentIcon />} />
          <BottomTab href="/me"     active={isActive("/me")}     label="Officer" icon={<MeIcon />} />
        </div>
      }
    >
      {children}
    </MobileShell>
  );
}

// ─── Inline SVG icons (kept here so the officer app has zero icon-pack dep)

function ScanIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="M3 7V5a2 2 0 0 1 2-2h2M21 7V5a2 2 0 0 0-2-2h-2M3 17v2a2 2 0 0 0 2 2h2M21 17v2a2 2 0 0 1-2 2h-2" />
      <line x1="3" y1="12" x2="21" y2="12" />
    </svg>
  );
}
function VerifyIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="M12 3l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7l8-4z"/>
      <path d="m9 12 2 2 4-4"/>
    </svg>
  );
}
function RecentIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <circle cx="12" cy="12" r="9"/>
      <path d="M12 7v5l3 2"/>
    </svg>
  );
}
function MeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <circle cx="12" cy="8" r="4"/>
      <path d="M4 21a8 8 0 0 1 16 0"/>
    </svg>
  );
}
function ProfileIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <circle cx="12" cy="9" r="3.5"/>
      <path d="M5 20a7 7 0 0 1 14 0"/>
    </svg>
  );
}
