// Minimal i18n. Each app ships its own locale dictionaries; this helper
// merely resolves the active locale from cookie/header/localStorage and
// provides a `t(key)` helper. For complex pluralization, swap to next-intl.

export type Locale = "en" | "fr" | "de" | "es" | "no" | "ar";

export const RTL_LOCALES: Locale[] = ["ar"];

export type Dict = Record<string, string>;

export function tFactory(dict: Dict, fallback: Dict) {
  return (key: string, vars?: Record<string, string | number>) => {
    let s = dict[key] ?? fallback[key] ?? key;
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        s = s.replaceAll(`{${k}}`, String(v));
      }
    }
    return s;
  };
}

export function detectLocale(): Locale {
  if (typeof window === "undefined") return "en";
  const stored = window.localStorage.getItem("naditos.locale") as Locale | null;
  if (stored) return stored;
  const nav = (navigator.language || "en").slice(0, 2) as Locale;
  return (["en","fr","de","es","no","ar"] as Locale[]).includes(nav) ? nav : "en";
}
