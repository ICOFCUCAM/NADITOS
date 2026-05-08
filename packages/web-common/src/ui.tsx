// A few primitive Tailwind components used across all three apps.
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";

export function Button(
  { children, className = "", ...rest }: ButtonHTMLAttributes<HTMLButtonElement>,
) {
  return (
    <button
      className={
        "inline-flex items-center justify-center rounded-md px-4 py-2 " +
        "text-sm font-medium bg-slate-900 text-white hover:bg-slate-800 " +
        "disabled:opacity-50 disabled:cursor-not-allowed " + className
      }
      {...rest}
    >
      {children}
    </button>
  );
}

export function Input(
  { className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>,
) {
  return (
    <input
      className={
        "block w-full rounded-md border border-slate-300 px-3 py-2 text-sm " +
        "focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500 " + className
      }
      {...rest}
    />
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={"rounded-lg border border-slate-200 bg-white p-5 shadow-sm " + className}>
      {children}
    </div>
  );
}

export function Pill({ tone = "slate", children }: { tone?: "slate"|"green"|"red"|"amber"|"black"; children: ReactNode }) {
  const cls = {
    slate: "bg-slate-100 text-slate-700",
    green: "bg-emerald-100 text-emerald-900",
    red:   "bg-red-100 text-red-900",
    amber: "bg-amber-100 text-amber-900",
    black: "bg-slate-900 text-white",
  }[tone];
  return (
    <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${cls}`}>
      {children}
    </span>
  );
}
