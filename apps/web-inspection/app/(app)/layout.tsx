"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  Brand, Button, CommandShell, Pill, SidebarItem, SidebarSection,
  StatusDot, useSession,
} from "@naditos/web-common";

// Inspection-authority shell. Same operations-console feel as the
// ministry app but the navigation reflects an inspection station's
// daily workflow: schedule and certify, then look up history.

const NAV = [
  {
    section: "Today",
    items: [
      { href: "/",           label: "Station",   icon: <StationIcon /> },
      { href: "/queue",      label: "Queue",     icon: <QueueIcon /> },
    ],
  },
  {
    section: "Records",
    items: [
      { href: "/inspections", label: "Inspections", icon: <ClipboardIcon /> },
      { href: "/vehicles",    label: "Vehicles",    icon: <VehicleIcon /> },
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
      brand={<Brand product="Inspection · Station" tenant={session.user.tenant} />}
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
              Inspector
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
            <Pill tone="gold">station · {session.user.tenant}</Pill>
          </div>
        </>
      }
    >
      {children}
    </CommandShell>
  );
}

function StationIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M3 21V8l9-5 9 5v13" />
      <path d="M9 21V12h6v9" />
    </svg>
  );
}
function QueueIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M3 6h18M3 12h18M3 18h12" />
    </svg>
  );
}
function ClipboardIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <rect x="6" y="4" width="12" height="17" rx="2" />
      <path d="M9 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1" />
      <path d="M9 11h6M9 15h6" />
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
