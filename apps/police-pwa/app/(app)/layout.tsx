"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useSession } from "@naditos/web-common";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { session, loading, logout } = useSession();
  const router = useRouter();
  useEffect(() => {
    if (!loading && !session) router.replace("/login");
  }, [loading, session, router]);
  if (loading || !session) return <div className="p-10 text-slate-400">Loading…</div>;
  return (
    <div className="min-h-screen flex flex-col">
      <header className="flex items-center justify-between px-4 py-3 bg-slate-800">
        <div className="text-sm font-bold">NADITOS · Officer</div>
        <div className="text-xs text-slate-400">{session.user.full_name}</div>
      </header>
      <main className="flex-1">{children}</main>
      <nav className="grid grid-cols-3 border-t border-slate-700 bg-slate-800">
        <NavLink href="/scan"   label="Scan" />
        <NavLink href="/recent" label="Recent" />
        <NavLink href="/me"     label="Me" onClick={() => logout().then(() => router.replace("/login"))} />
      </nav>
    </div>
  );
}

function NavLink({ href, label, onClick }: { href: string; label: string; onClick?: () => void }) {
  if (onClick) return (
    <button onClick={onClick} className="py-3 text-sm hover:bg-slate-700">{label}</button>
  );
  return (
    <Link href={href} className="py-3 text-sm text-center hover:bg-slate-700">{label}</Link>
  );
}
