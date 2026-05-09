"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { Pill, useSession } from "@naditos/web-common";

// Citizen portal shell.
//
// Trustworthy, document-feel: a slim institutional header carrying
// the citizen's name and jurisdiction badge, plus a sticky tab row
// that maps to the four real-world artefacts a citizen owns —
// Vehicles, License, Fines, Inbox. Profile + sign-out live behind a
// small "Account" affordance so the page is dominated by content.

const NAV = [
  { href: "/",          label: "Home",     icon: <HomeIcon /> },
  { href: "/vehicles",  label: "Vehicles", icon: <CarIcon /> },
  { href: "/license",   label: "License",  icon: <LicIcon /> },
  { href: "/fines",     label: "Fines",    icon: <FineIcon /> },
  { href: "/inbox",     label: "Inbox",    icon: <BellIcon /> },
];

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, loading, logout } = useSession();
  const router = useRouter();
  const pathname = usePathname() || "";
  useEffect(() => {
    if (!loading && !session) router.replace("/login");
  }, [loading, session, router]);
  if (loading || !session) {
    return <div className="p-10 text-[var(--fg-muted)]">Loading…</div>;
  }

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : (pathname === href || pathname.startsWith(href + "/"));

  return (
    <div className="min-h-screen">
      <header className="bg-[var(--bg-surface)] border-b border-[var(--border-subtle)]
                         shadow-[var(--e-soft)]">
        <div className="max-w-4xl mx-auto px-4 sm:px-6 py-3 flex items-center justify-between">
          <Link href="/" className="flex items-center gap-2.5 group">
            <span aria-hidden
              className="h-8 w-8 rounded-[var(--r-sm)]
                         bg-[var(--accent-primary)] text-[var(--accent-primary-fg)]
                         grid place-items-center font-bold text-[13px]"
              style={{ fontFamily: "var(--ff-display)" }}>N</span>
            <div className="leading-tight">
              <div className="text-[14px] font-semibold tracking-[0.04em]"
                   style={{ fontFamily: "var(--ff-display)" }}>NADITOS</div>
              <div className="text-[10px] uppercase tracking-[0.16em] text-[var(--fg-muted)]">
                Citizen portal
              </div>
            </div>
          </Link>
          <div className="flex items-center gap-3">
            <div className="hidden sm:flex flex-col items-end leading-tight">
              <div className="text-[13px] text-[var(--fg-primary)]">{session.user.full_name}</div>
              <Pill tone="ops">{session.user.tenant}</Pill>
            </div>
            <button onClick={() => logout().then(() => router.replace("/login"))}
              className="text-[12px] uppercase tracking-[0.12em] text-[var(--fg-muted)]
                         hover:text-[var(--fg-primary)]
                         focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)]
                         rounded-[var(--r-sm)] px-2 py-1">
              Sign out
            </button>
          </div>
        </div>
        <nav className="max-w-4xl mx-auto px-4 sm:px-6 flex items-center gap-1 overflow-x-auto">
          {NAV.map((n) => {
            const active = isActive(n.href);
            return (
              <Link key={n.href} href={n.href}
                aria-current={active ? "page" : undefined}
                className={
                  "inline-flex items-center gap-2 px-3 py-2.5 text-[13px] font-medium " +
                  "border-b-2 transition-[color,border-color] " +
                  "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
                  (active
                    ? "border-[var(--accent-primary)] text-[var(--fg-primary)]"
                    : "border-transparent text-[var(--fg-muted)] hover:text-[var(--fg-primary)]")
                }>
                <span className="h-4 w-4">{n.icon}</span>
                {n.label}
              </Link>
            );
          })}
        </nav>
      </header>
      <main className="max-w-4xl mx-auto px-4 sm:px-6 py-6 space-y-5">{children}</main>
    </div>
  );
}

function HomeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="m3 11 9-8 9 8v9a2 2 0 0 1-2 2h-4v-7H9v7H5a2 2 0 0 1-2-2z"/>
    </svg>
  );
}
function CarIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="M5 17h14M3 13l2-6h14l2 6"/>
      <circle cx="7" cy="17" r="2"/><circle cx="17" cy="17" r="2"/>
    </svg>
  );
}
function LicIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <rect x="3" y="5" width="18" height="14" rx="2"/>
      <circle cx="9" cy="12" r="2"/><path d="M14 11h4M14 14h4"/>
    </svg>
  );
}
function FineIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/>
      <path d="M14 3v5h5"/>
    </svg>
  );
}
function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
         strokeLinecap="round" strokeLinejoin="round" className="h-full w-full">
      <path d="M6 8a6 6 0 0 1 12 0v5l2 3H4l2-3z"/><path d="M10 19a2 2 0 0 0 4 0"/>
    </svg>
  );
}
