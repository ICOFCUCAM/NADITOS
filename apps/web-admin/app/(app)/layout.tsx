"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  Brand, Button, CommandShell, IconButton, Pill, SidebarItem, SidebarSection,
  StatusDot, useSession,
} from "@naditos/web-common";

// Ministry command-center shell.
//
// Three-zone layout: left sidebar grouped by operational domain,
// sticky topbar with global search affordance + jurisdiction
// indicator + sign-out, content well below. All groupings reflect
// the actual mental model of a ministry duty officer rather than a
// flat alphabetical menu.

const NAV: { section: string; items: { href: string; label: string; icon: React.ReactNode }[] }[] = [
  {
    section: "Operations",
    items: [
      { href: "/",          label: "Command",      icon: <CommandIcon /> },
      { href: "/anpr",      label: "ANPR feed",    icon: <RadarIcon /> },
      { href: "/officers",  label: "Officer activity", icon: <BadgeIcon /> },
    ],
  },
  {
    section: "Registry",
    items: [
      { href: "/vehicles",  label: "Vehicles",        icon: <VehicleIcon /> },
      { href: "/licenses",  label: "Driver licenses", icon: <LicenseIcon /> },
    ],
  },
  {
    section: "Enforcement",
    items: [
      { href: "/fines",     label: "Fines",     icon: <FineIcon /> },
      { href: "/disputes",  label: "Disputes",  icon: <DisputeIcon /> },
    ],
  },
  {
    section: "System",
    items: [
      { href: "/providers",     label: "Provider health", icon: <PulseIcon /> },
      { href: "/notifications", label: "Notifications",   icon: <BellIcon /> },
      { href: "/audit",         label: "Audit ledger",    icon: <SealIcon /> },
    ],
  },
];

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, loading, logout } = useSession();
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
    href === "/" ? pathname === "/" : (pathname === href || pathname.startsWith(href + "/"));

  return (
    <CommandShell
      brand={<Brand product="Ministry · Command" tenant={session.user.tenant} />}
      sidebar={
        <>
          {NAV.map((g) => (
            <SidebarSection key={g.section} label={g.section}>
              {g.items.map((it) => (
                <SidebarItem key={it.href} href={it.href} active={isActive(it.href)}
                  label={it.label} icon={it.icon} />
              ))}
            </SidebarSection>
          ))}
          <div className="mt-2 px-3 py-3 rounded-[var(--r-md)] bg-[var(--bg-elevated)]
                          ring-1 ring-[var(--border-subtle)]">
            <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">
              Duty officer
            </div>
            <div className="text-sm mt-0.5 text-[var(--fg-primary)] truncate">
              {session.user.full_name}
            </div>
            <div className="mt-1 mb-3"><Pill tone="ops">{session.user.role}</Pill></div>
            <Button tone="secondary" size="sm" fullWidth
              onClick={() => logout().then(() => router.replace("/login"))}>
              Sign out
            </Button>
          </div>
        </>
      }
      topbar={
        <>
          <div className="flex items-center gap-2 text-[11px] uppercase tracking-[0.16em]
                          text-[var(--fg-muted)]">
            <StatusDot tone="ops" pulse />
            Live · {new Date().toLocaleDateString(undefined, { weekday: "short", year: "numeric", month: "short", day: "numeric" })}
          </div>
          <div className="ml-auto flex items-center gap-2">
            <Pill tone="gold">jurisdiction · {session.user.tenant}</Pill>
            <IconButton label="Open command palette" onClick={() => {/* phase-2 */}}>
              <CmdIcon />
            </IconButton>
          </div>
        </>
      }
    >
      {children}
    </CommandShell>
  );
}

// ─── Icons (local to admin shell) ────────────────────────────────────────

function CommandIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <rect x="3" y="3" width="7" height="7" rx="1.2" />
      <rect x="14" y="3" width="7" height="7" rx="1.2" />
      <rect x="3" y="14" width="7" height="7" rx="1.2" />
      <rect x="14" y="14" width="7" height="7" rx="1.2" />
    </svg>
  );
}
function VehicleIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M5 17h14M3 13l2-6h14l2 6"/>
      <circle cx="7" cy="17" r="2"/><circle cx="17" cy="17" r="2"/>
    </svg>
  );
}
function LicenseIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <rect x="3" y="5" width="18" height="14" rx="2"/>
      <circle cx="9" cy="12" r="2"/><path d="M14 11h4M14 14h4"/>
    </svg>
  );
}
function FineIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/>
      <path d="M14 3v5h5M9 13h6M9 17h4"/>
    </svg>
  );
}
function DisputeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M21 12a9 9 0 1 1-3-6.7"/><path d="m17 4 4 4-4 4"/>
    </svg>
  );
}
function RadarIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <circle cx="12" cy="12" r="9"/>
      <path d="M12 12 4 7"/><circle cx="12" cy="12" r="4"/>
    </svg>
  );
}
function BadgeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M12 3l9 5v5c0 5-4 8-9 8s-9-3-9-8V8z"/>
      <circle cx="12" cy="12" r="2"/>
    </svg>
  );
}
function PulseIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M3 12h4l2-7 4 14 2-7h6"/>
    </svg>
  );
}
function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M6 8a6 6 0 0 1 12 0v5l2 3H4l2-3z"/><path d="M10 19a2 2 0 0 0 4 0"/>
    </svg>
  );
}
function SealIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <circle cx="12" cy="9" r="5"/><path d="m9 13-2 8 5-3 5 3-2-8"/>
    </svg>
  );
}
function CmdIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M9 9h6v6H9z"/>
      <path d="M9 9V6a2 2 0 1 0-2 2h2zM9 15v3a2 2 0 1 1-2-2h2zM15 9h3a2 2 0 1 0-2-2v2zM15 15v3a2 2 0 1 1-2-2h2z"/>
    </svg>
  );
}
