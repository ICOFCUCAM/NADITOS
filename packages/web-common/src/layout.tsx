// Layout primitives shared by the three NADITOS apps.
//
// Each app composes these instead of hand-rolling its shell. The
// surface (.nadit-dark / .nadit-light) is set by the app at the
// top-level <html> or <body>, and these primitives inherit token
// values without branching.

import type { ReactNode } from "react";

// ─── CommandShell — sidebar + topbar + main, dark-first ────────────────

export function CommandShell({
  brand, sidebar, topbar, children,
}: {
  brand: ReactNode;
  sidebar: ReactNode;
  topbar?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="min-h-screen flex bg-[var(--bg-canvas)]">
      <aside
        className="w-64 shrink-0 border-r border-[var(--border-subtle)]
                   bg-[var(--bg-surface)] flex flex-col"
      >
        <div className="px-5 pt-5 pb-3 border-b border-[var(--border-subtle)]">
          {brand}
        </div>
        <nav className="flex-1 px-3 py-4 overflow-y-auto">{sidebar}</nav>
      </aside>
      <div className="flex-1 flex flex-col min-w-0">
        {topbar && (
          <header
            className="h-14 shrink-0 border-b border-[var(--border-subtle)]
                       bg-[var(--bg-surface)]/80 backdrop-blur-md
                       flex items-center px-6 gap-4 sticky top-0 z-20"
          >
            {topbar}
          </header>
        )}
        <main className="flex-1 px-6 py-6 overflow-x-hidden">{children}</main>
      </div>
    </div>
  );
}

// ─── SidebarNav: ministry-style left nav with section grouping ────────

export function SidebarSection({
  label, children,
}: { label: string; children: ReactNode }) {
  return (
    <div className="mb-5">
      <div className="px-3 mb-1.5 text-[10px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">
        {label}
      </div>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

export function SidebarItem({
  href, active, label, icon, count,
}: {
  href: string;
  active?: boolean;
  label: ReactNode;
  icon?: ReactNode;
  count?: number;
}) {
  return (
    <a
      href={href}
      aria-current={active ? "page" : undefined}
      className={
        "group flex items-center gap-3 rounded-[var(--r-md)] px-3 py-2 text-sm " +
        "transition-[background,color] duration-[var(--m-fast)] " +
        "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
        (active
          ? "bg-[var(--accent-soft-bg)] text-[var(--accent-soft-fg)] " +
            "ring-1 ring-[var(--accent-primary)]/30"
          : "text-[var(--fg-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--fg-primary)]")
      }
    >
      {icon && (
        <span className={
          "h-5 w-5 grid place-items-center " +
          (active ? "text-[var(--accent-primary)]" : "text-[var(--fg-muted)]")
        }>{icon}</span>
      )}
      <span className="flex-1 truncate">{label}</span>
      {typeof count === "number" && (
        <span className={
          "text-[11px] tabular-nums " +
          (active ? "text-[var(--accent-soft-fg)]" : "text-[var(--fg-muted)]")
        }>{count}</span>
      )}
    </a>
  );
}

// ─── Brand — the top-of-sidebar mark used across apps ─────────────────

export function Brand({
  product, tenant,
}: { product: string; tenant?: string }) {
  return (
    <div className="flex items-center gap-3">
      <span
        aria-hidden
        className="h-9 w-9 rounded-[var(--r-md)]
                   bg-[var(--accent-primary)] text-[var(--accent-primary-fg)]
                   grid place-items-center font-bold text-sm tracking-tighter"
        style={{
          fontFamily: "var(--ff-display)",
          boxShadow: "var(--glow-ops)",
        }}
      >
        N
      </span>
      <div className="leading-tight">
        <div
          className="text-[15px] font-semibold tracking-[0.04em] text-[var(--fg-primary)]"
          style={{ fontFamily: "var(--ff-display)" }}
        >
          NADITOS
        </div>
        <div className="text-[11px] text-[var(--fg-muted)] tracking-wider uppercase">
          {product}
          {tenant && <> · <span className="text-[var(--fg-secondary)]">{tenant}</span></>}
        </div>
      </div>
    </div>
  );
}

// ─── MobileShell — police-PWA, header + scroll body + bottom tabs ─────

export function MobileShell({
  topbar, children, bottom,
}: {
  topbar?: ReactNode;
  children: ReactNode;
  bottom?: ReactNode;
}) {
  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)]">
      {topbar && (
        <header
          className="sticky top-0 z-20 h-14 border-b border-[var(--border-subtle)]
                     bg-[var(--bg-surface)]/80 backdrop-blur-md
                     flex items-center justify-between px-4"
        >
          {topbar}
        </header>
      )}
      <main className="flex-1 overflow-y-auto pb-20">{children}</main>
      {bottom && (
        <nav
          className="fixed bottom-0 left-0 right-0 z-30
                     border-t border-[var(--border-default)]
                     bg-[var(--bg-surface)]/95 backdrop-blur-md
                     safe-area-padding"
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          {bottom}
        </nav>
      )}
    </div>
  );
}

// ─── BottomTab — touch-first 56px tap target ───────────────────────────

export function BottomTab({
  href, active, label, icon,
}: {
  href: string;
  active?: boolean;
  label: string;
  icon: ReactNode;
}) {
  return (
    <a
      href={href}
      aria-current={active ? "page" : undefined}
      className={
        "flex flex-col items-center justify-center gap-0.5 py-2 " +
        "min-h-[56px] " +
        "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
        (active
          ? "text-[var(--accent-primary)]"
          : "text-[var(--fg-muted)] hover:text-[var(--fg-primary)]")
      }
    >
      <span className="h-6 w-6">{icon}</span>
      <span className="text-[10px] uppercase tracking-[0.10em] font-medium">{label}</span>
      <span
        aria-hidden
        className={
          "h-[2px] w-6 rounded-full transition-opacity " +
          (active ? "bg-[var(--accent-primary)] opacity-100" : "opacity-0")
        }
      />
    </a>
  );
}

// ─── PageContainer — consistent max-width content well ─────────────────

export function PageContainer({
  children, className = "", size = "default",
}: {
  children: ReactNode;
  className?: string;
  size?: "default" | "narrow" | "wide";
}) {
  const max = size === "narrow" ? "max-w-3xl" : size === "wide" ? "max-w-7xl" : "max-w-6xl";
  return (
    <div className={`mx-auto w-full ${max} ${className}`}>{children}</div>
  );
}
