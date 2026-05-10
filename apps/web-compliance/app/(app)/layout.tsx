"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  Brand, Button, CommandShell, Pill, SidebarItem, SidebarSection,
  StatusDot, useSession,
} from "@naditos/web-common";

// Audit & Compliance shell. Auditor-facing — append-only ledger,
// alerts queue, compliance reports. The right-hand pill shows
// "court order" affordance because every drilldown is a privileged
// action that lands in the audit log itself.

const NAV = [
  {
    section: "Ledger",
    items: [
      { href: "/",         label: "Overview",     icon: <SealIcon /> },
      { href: "/events",   label: "Audit events", icon: <BookIcon /> },
      { href: "/verify",   label: "Verify chain", icon: <ChainIcon /> },
    ],
  },
  {
    section: "Compliance",
    items: [
      { href: "/alerts",   label: "Alerts",       icon: <AlertIcon /> },
      { href: "/reports",  label: "Reports",      icon: <ReportIcon /> },
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
      brand={<Brand product="Audit · Compliance" tenant={session.user.tenant} />}
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
              Auditor
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
            Ledger · sealed
          </div>
          <div className="ml-auto flex items-center gap-2">
            <Pill tone="gold">jurisdiction · {session.user.tenant}</Pill>
          </div>
        </>
      }
    >
      {children}
    </CommandShell>
  );
}

function SealIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <circle cx="12" cy="9" r="5" />
      <path d="m9 13-2 8 5-3 5 3-2-8" />
    </svg>
  );
}
function BookIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M4 4v16a2 2 0 0 0 2 2h14V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2z" />
      <path d="M8 4v18" />
    </svg>
  );
}
function ChainIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 1 0-5.66-5.66L11 7" />
      <path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 1 0 5.66 5.66L13 17" />
    </svg>
  );
}
function AlertIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="m12 2 10 18H2z" />
      <path d="M12 9v5M12 17v.5" />
    </svg>
  );
}
function ReportIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M3 3v18h18" />
      <path d="M7 14l4-4 3 3 5-6" />
    </svg>
  );
}
