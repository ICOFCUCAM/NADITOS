// NADITOS GovTech component primitives.
//
// Components consume CSS variables defined in tokens.css. Each surface
// (.nadit-dark / .nadit-light) re-binds the same semantic names, so a
// component like <Card> renders correctly in either context without
// branching internally. New components belong here unless they're
// app-specific workflow surfaces.
//
// Accessibility baseline: all interactive elements have visible focus
// rings, aria-disabled instead of pointer-events:none, AA contrast
// enforced through tokens, and keyboard-reachable hit targets ≥ 44px
// on mobile via .nadit-touch.
import type {
  AnchorHTMLAttributes,
  ButtonHTMLAttributes,
  HTMLAttributes,
  InputHTMLAttributes,
  LabelHTMLAttributes,
  ReactNode,
} from "react";

// ─── Button ─────────────────────────────────────────────────────────────

type ButtonTone = "primary" | "secondary" | "ghost" | "danger" | "warn";
type ButtonSize = "sm" | "md" | "lg" | "field";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  tone?: ButtonTone;
  size?: ButtonSize;
  iconLeft?: ReactNode;
  iconRight?: ReactNode;
  fullWidth?: boolean;
}

const buttonBase =
  "inline-flex items-center justify-center gap-2 select-none " +
  "rounded-[var(--r-md)] font-medium tracking-[0.01em] " +
  "transition-[background,box-shadow,transform] duration-[var(--m-fast)] " +
  "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
  "disabled:opacity-50 disabled:cursor-not-allowed disabled:saturate-50 " +
  "active:scale-[0.98]";

const buttonByTone: Record<ButtonTone, string> = {
  primary:
    "bg-[var(--accent-primary)] text-[var(--accent-primary-fg)] " +
    "hover:bg-[var(--accent-strong)] " +
    "shadow-[0_0_0_1px_rgba(255,255,255,0.04),var(--glow-ops)]",
  secondary:
    "bg-[var(--bg-elevated)] text-[var(--fg-primary)] " +
    "ring-1 ring-[var(--border-default)] hover:bg-[var(--bg-hover)] " +
    "hover:ring-[var(--border-strong)]",
  ghost:
    "bg-transparent text-[var(--fg-secondary)] hover:bg-[var(--bg-hover)] " +
    "hover:text-[var(--fg-primary)]",
  danger:
    "bg-[var(--c-bad-600)] text-white hover:bg-[var(--c-bad-700)] " +
    "shadow-[var(--glow-bad)]",
  warn:
    "bg-[var(--c-warn-500)] text-[#1a1100] hover:bg-[var(--c-warn-600)] hover:text-white",
};

const buttonBySize: Record<ButtonSize, string> = {
  sm:    "h-8  px-3 text-[13px]",
  md:    "h-10 px-4 text-sm",
  lg:    "h-12 px-5 text-[15px]",
  field: "h-14 px-5 text-base", // one-handed, gloves-on field tap target
};

export function Button({
  tone = "primary",
  size = "md",
  iconLeft,
  iconRight,
  fullWidth,
  className = "",
  children,
  ...rest
}: ButtonProps) {
  const cls =
    `${buttonBase} ${buttonByTone[tone]} ${buttonBySize[size]} ` +
    (fullWidth ? "w-full " : "") + className;
  return (
    <button className={cls} {...rest}>
      {iconLeft && <span className="-ml-0.5 inline-flex items-center">{iconLeft}</span>}
      {children}
      {iconRight && <span className="-mr-0.5 inline-flex items-center">{iconRight}</span>}
    </button>
  );
}

// ─── IconButton ─────────────────────────────────────────────────────────

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string; // required for aria-label — never silent
  size?: "sm" | "md" | "lg";
  tone?: "ghost" | "primary" | "danger";
}

export function IconButton({
  label, size = "md", tone = "ghost", className = "", children, ...rest
}: IconButtonProps) {
  const dim = size === "sm" ? "h-8 w-8" : size === "lg" ? "h-12 w-12" : "h-10 w-10";
  const toneCls =
    tone === "primary"
      ? "bg-[var(--accent-primary)] text-[var(--accent-primary-fg)] hover:bg-[var(--accent-strong)]"
      : tone === "danger"
      ? "bg-[var(--c-bad-600)] text-white hover:bg-[var(--c-bad-700)]"
      : "text-[var(--fg-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--fg-primary)]";
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={
        "inline-flex items-center justify-center rounded-[var(--r-md)] " +
        "transition-[background,color] duration-[var(--m-fast)] " +
        "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
        "disabled:opacity-50 disabled:cursor-not-allowed " +
        `${dim} ${toneCls} ${className}`
      }
      {...rest}
    >
      {children}
    </button>
  );
}

// ─── Input + Label + Field ──────────────────────────────────────────────

export function Label({ className = "", children, ...rest }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label
      className={
        "block text-[13px] font-medium tracking-[0.02em] uppercase " +
        "text-[var(--fg-muted)] " + className
      }
      {...rest}
    >
      {children}
    </label>
  );
}

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean;
  inputSize?: "md" | "lg" | "field";
}

export function Input({
  className = "", invalid, inputSize = "md", ...rest
}: InputProps) {
  const dim =
    inputSize === "field" ? "h-14 px-4 text-base" :
    inputSize === "lg"    ? "h-12 px-4 text-[15px]" :
                            "h-10 px-3 text-sm";
  return (
    <input
      aria-invalid={invalid || undefined}
      className={
        "block w-full rounded-[var(--r-md)] " +
        "bg-[var(--bg-elevated)] text-[var(--fg-primary)] " +
        "ring-1 placeholder:text-[var(--fg-muted)] " +
        "transition-[box-shadow,background] duration-[var(--m-fast)] " +
        "focus:outline-none focus:bg-[var(--bg-elevated)] " +
        "focus-visible:[box-shadow:var(--focus-ring)] " +
        "disabled:opacity-60 disabled:cursor-not-allowed " +
        (invalid
          ? "ring-[var(--c-bad-500)] focus-visible:[box-shadow:0_0_0_3px_rgba(239,68,68,0.30)] "
          : "ring-[var(--border-default)] hover:ring-[var(--border-strong)] focus:ring-[var(--accent-primary)] ") +
        dim + " " + className
      }
      {...rest}
    />
  );
}

interface FieldProps {
  label: string;
  hint?: ReactNode;
  error?: string | null;
  children: ReactNode;
  className?: string;
}

export function Field({ label, hint, error, children, className = "" }: FieldProps) {
  return (
    <div className={"space-y-1.5 " + className}>
      <Label>{label}</Label>
      {children}
      {error && (
        <p className="text-[13px] text-[var(--c-bad-300)] flex items-center gap-1">
          <span aria-hidden>⚠</span> {error}
        </p>
      )}
      {!error && hint && (
        <p className="text-[12px] text-[var(--fg-muted)]">{hint}</p>
      )}
    </div>
  );
}

// ─── Card / Panel / SectionHeader ───────────────────────────────────────

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  tone?: "default" | "elevated" | "outline" | "alert";
  pad?: "none" | "sm" | "md" | "lg";
}

export function Card({
  tone = "default", pad = "md", className = "", children, ...rest
}: CardProps) {
  const padCls =
    pad === "none" ? "" :
    pad === "sm"   ? "p-4" :
    pad === "lg"   ? "p-7" :
                     "p-5";
  const toneCls =
    tone === "elevated"
      ? "bg-[var(--bg-elevated)] ring-1 ring-[var(--border-subtle)] shadow-[var(--e-raised)]"
      : tone === "outline"
      ? "bg-transparent ring-1 ring-[var(--border-default)]"
      : tone === "alert"
      ? "bg-[var(--status-bad-bg)] ring-1 ring-[var(--c-bad-500)] text-[var(--status-bad-fg)]"
      : "bg-[var(--bg-surface)] ring-1 ring-[var(--border-subtle)] shadow-[var(--e-soft)]";
  return (
    <div
      className={`rounded-[var(--r-lg)] ${toneCls} ${padCls} ${className}`}
      {...rest}
    >
      {children}
    </div>
  );
}

interface SectionHeaderProps {
  eyebrow?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function SectionHeader({
  eyebrow, title, description, actions, className = "",
}: SectionHeaderProps) {
  return (
    <div className={"flex items-start justify-between gap-4 " + className}>
      <div className="min-w-0">
        {eyebrow && (
          <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-1">
            {eyebrow}
          </div>
        )}
        <h1 className="text-[var(--t-h1)] font-semibold text-[var(--fg-primary)] leading-tight"
            style={{ fontFamily: "var(--ff-display)" }}>
          {title}
        </h1>
        {description && (
          <p className="mt-1.5 text-[var(--t-body)] text-[var(--fg-secondary)] max-w-prose">
            {description}
          </p>
        )}
      </div>
      {actions && <div className="shrink-0 flex items-center gap-2">{actions}</div>}
    </div>
  );
}

// ─── Pill / StatusDot / StatusBadge ─────────────────────────────────────

type Tone = "slate" | "green" | "red" | "amber" | "black" | "ops" | "gold";

const pillByTone: Record<Tone, string> = {
  slate: "bg-[var(--bg-hover)] text-[var(--fg-secondary)] ring-[var(--border-default)]",
  green: "bg-[var(--status-ok-bg)]   text-[var(--status-ok-fg)]   ring-[var(--c-ok-500)]/30",
  red:   "bg-[var(--status-bad-bg)]  text-[var(--status-bad-fg)]  ring-[var(--c-bad-500)]/30",
  amber: "bg-[var(--status-warn-bg)] text-[var(--status-warn-fg)] ring-[var(--c-warn-500)]/30",
  black: "bg-[var(--status-alert-bg)] text-[var(--status-alert-fg)] ring-black",
  ops:   "bg-[var(--accent-soft-bg)] text-[var(--accent-soft-fg)] ring-[var(--accent-primary)]/30",
  gold:  "bg-[var(--gold-soft)]      text-[var(--gold)]            ring-[var(--gold)]/40",
};

export function Pill({
  tone = "slate", children,
}: { tone?: Tone; children: ReactNode }) {
  return (
    <span className={
      "inline-flex items-center gap-1 rounded-[var(--r-pill)] " +
      "px-2.5 py-0.5 text-[11px] font-medium uppercase tracking-[0.06em] " +
      "ring-1 " + pillByTone[tone]
    }>
      {children}
    </span>
  );
}

export function StatusDot({ tone = "slate", pulse }: { tone?: Tone; pulse?: boolean }) {
  const color = {
    slate: "var(--fg-muted)",
    green: "var(--c-ok-500)",
    red:   "var(--c-bad-500)",
    amber: "var(--c-warn-500)",
    black: "#000",
    ops:   "var(--accent-primary)",
    gold:  "var(--gold)",
  }[tone];
  return (
    <span className="relative inline-flex h-2.5 w-2.5">
      {pulse && (
        <span
          className="absolute inset-0 rounded-full opacity-50 animate-ping"
          style={{ background: color }}
          aria-hidden
        />
      )}
      <span className="relative inline-flex h-2.5 w-2.5 rounded-full"
            style={{ background: color, boxShadow: `0 0 0 2px ${color}33` }} />
    </span>
  );
}

// ─── Plate — the most-rendered piece of data in the system ──────────────

export function Plate({
  value, size = "md",
}: { value: string; size?: "sm" | "md" | "lg" | "xl" }) {
  const sizeCls =
    size === "sm" ? "text-[13px] px-2 py-0.5" :
    size === "lg" ? "text-xl   px-3 py-1"   :
    size === "xl" ? "text-3xl  px-4 py-1.5" :
                    "text-[15px] px-2.5 py-0.5";
  return (
    <span
      className={
        "inline-flex items-center rounded-[var(--r-sm)] font-semibold " +
        "tracking-[0.10em] " + sizeCls + " " +
        "bg-[var(--gold-soft)] text-[var(--gold)] " +
        "ring-1 ring-[var(--gold)]/40 " +
        "shadow-[inset_0_-1px_0_rgba(0,0,0,0.10)]"
      }
      style={{ fontFamily: "var(--ff-mono)" }}
    >
      {value}
    </span>
  );
}

// ─── Mono — for plate, license, hashes, IDs ─────────────────────────────

export function Mono({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <span
      className={"text-[var(--signal-mono-fg)] " + className}
      style={{ fontFamily: "var(--ff-mono)" }}
    >
      {children}
    </span>
  );
}

// ─── KeyValue — dense info display, common in dashboards ───────────────

export function KeyValue({
  k, v, mono, tone,
}: { k: ReactNode; v: ReactNode; mono?: boolean; tone?: "default" | "muted" }) {
  return (
    <div className="flex flex-col gap-0.5 min-w-0">
      <div className="text-[11px] uppercase tracking-[0.10em] text-[var(--fg-muted)]">
        {k}
      </div>
      <div
        className={
          (tone === "muted" ? "text-[var(--fg-muted)] " : "text-[var(--fg-primary)] ") +
          "text-sm truncate"
        }
        style={mono ? { fontFamily: "var(--ff-mono)" } : undefined}
      >
        {v}
      </div>
    </div>
  );
}

// ─── Link (next/link wrapper for non-Next contexts) ─────────────────────

export function TextLink({
  className = "", children, ...rest
}: AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a
      className={
        "text-[var(--accent-soft-fg)] hover:text-[var(--accent-strong)] " +
        "underline decoration-[var(--accent-primary)]/40 underline-offset-4 " +
        "focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)] " +
        "rounded-[var(--r-xs)] " + className
      }
      {...rest}
    >
      {children}
    </a>
  );
}

// ─── Skeleton / EmptyState ─────────────────────────────────────────────

export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <div
      className={
        "rounded-[var(--r-sm)] bg-[var(--bg-hover)] animate-pulse " + className
      }
      aria-hidden
    />
  );
}

interface EmptyStateProps {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
}

export function EmptyState({ title, description, action, icon }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center text-center px-6 py-12 gap-3">
      {icon && (
        <div className="h-12 w-12 rounded-[var(--r-xl)] bg-[var(--bg-hover)]
                        text-[var(--fg-muted)] grid place-items-center">
          {icon}
        </div>
      )}
      <div className="text-[var(--fg-primary)] font-medium">{title}</div>
      {description && (
        <div className="text-[var(--fg-muted)] text-sm max-w-md">{description}</div>
      )}
      {action && <div className="pt-1">{action}</div>}
    </div>
  );
}

// ─── Stat — dashboard headline number ──────────────────────────────────

interface StatProps {
  label: string;
  value: ReactNode;
  delta?: { value: ReactNode; tone: "ok" | "warn" | "bad" | "muted" };
  hint?: ReactNode;
}

export function Stat({ label, value, delta, hint }: StatProps) {
  const dt =
    delta?.tone === "ok"   ? "text-[var(--status-ok-fg)]" :
    delta?.tone === "warn" ? "text-[var(--status-warn-fg)]" :
    delta?.tone === "bad"  ? "text-[var(--status-bad-fg)]" :
                             "text-[var(--fg-muted)]";
  return (
    <div className="space-y-1">
      <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)]">
        {label}
      </div>
      <div className="flex items-baseline gap-2">
        <div
          className="text-[var(--t-h1)] font-semibold leading-none text-[var(--fg-primary)]"
          style={{ fontFamily: "var(--ff-display)" }}
        >
          {value}
        </div>
        {delta && <div className={"text-xs " + dt}>{delta.value}</div>}
      </div>
      {hint && <div className="text-xs text-[var(--fg-muted)]">{hint}</div>}
    </div>
  );
}

// ─── Toolbar — used in command-center dashboards ───────────────────────

export function Toolbar({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={
      "flex flex-wrap items-center gap-2 rounded-[var(--r-md)] " +
      "bg-[var(--bg-surface)] ring-1 ring-[var(--border-subtle)] px-3 py-2 " +
      className
    }>
      {children}
    </div>
  );
}

export function ToolbarSep() {
  return (
    <span className="mx-1 h-5 w-px bg-[var(--border-default)]" aria-hidden />
  );
}
