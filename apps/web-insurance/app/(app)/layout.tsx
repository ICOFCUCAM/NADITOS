"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  Brand, Button, CommandShell, Pill, SidebarItem, SidebarSection,
  StatusDot, useSession,
} from "@naditos/web-common";

// Insurance integration shell. Provider-facing — the navigation is
// what an insurance partner needs to push policy lifecycle events
// into the registry and review their own delivery health.

const NAV = [
  {
    section: "Provider",
    items: [
      { href: "/",          label: "Overview",  icon: <BuildingIcon /> },
      { href: "/webhooks",  label: "Webhooks",  icon: <WebhookIcon /> },
    ],
  },
  {
    section: "Coverage",
    items: [
      { href: "/policies",  label: "Policies",  icon: <PolicyIcon /> },
      { href: "/claims",    label: "Claims",    icon: <ClaimIcon /> },
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
      brand={<Brand product="Insurance · Partner" tenant={session.user.tenant} />}
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
              Partner contact
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
            API · live
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

function BuildingIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M4 21V5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v16" />
      <path d="M9 21V11h6v10" />
      <path d="M9 7h2M13 7h2" />
    </svg>
  );
}
function WebhookIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 1 1 8 12.5c.55 0 1.07.11 1.54.31" />
      <path d="m11 5 3 4M9 11l3-4" />
    </svg>
  );
}
function PolicyIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5M9 13h6M9 17h4" />
    </svg>
  );
}
function ClaimIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M12 2 4 6v6c0 5 3.5 8 8 10 4.5-2 8-5 8-10V6z" />
    </svg>
  );
}
