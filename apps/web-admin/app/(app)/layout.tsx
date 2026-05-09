"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useSession } from "@naditos/web-common";

const NAV = [
  { href: "/",          label: "Dashboard" },
  { href: "/vehicles",  label: "Vehicles" },
  { href: "/licenses",  label: "Driver licenses" },
  { href: "/fines",     label: "Fines" },
  { href: "/disputes",  label: "Disputes" },
  { href: "/anpr",      label: "ANPR feed" },
  { href: "/officers",  label: "Officer activity" },
  { href: "/providers", label: "Provider health" },
  { href: "/notifications", label: "Notifications" },
  { href: "/audit",     label: "Audit" },
];

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, loading, logout } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (!loading && !session) router.replace("/login");
  }, [loading, session, router]);

  if (loading || !session) {
    return <div className="p-10 text-slate-500">Loading…</div>;
  }

  return (
    <div className="min-h-screen flex">
      <aside className="w-60 border-r border-slate-200 bg-white p-5">
        <div className="mb-6">
          <div className="text-lg font-bold">NADITOS</div>
          <div className="text-xs text-slate-500">{session.user.tenant}</div>
        </div>
        <nav className="space-y-1">
          {NAV.map((n) => (
            <Link key={n.href} href={n.href}
              className="block rounded px-3 py-2 text-sm hover:bg-slate-100">
              {n.label}
            </Link>
          ))}
        </nav>
        <div className="mt-8 border-t border-slate-200 pt-4 text-xs text-slate-500">
          <div className="font-medium text-slate-700">{session.user.full_name}</div>
          <div>{session.user.role}</div>
          <button onClick={() => logout().then(() => router.replace("/login"))}
            className="mt-3 text-xs underline text-slate-600">Sign out</button>
        </div>
      </aside>
      <main className="flex-1 p-8">{children}</main>
    </div>
  );
}
