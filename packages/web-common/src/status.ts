// Vehicle-status colour helpers shared by all apps.

export type VehicleStatus = "green" | "yellow" | "red" | "black";

export const statusLabel: Record<VehicleStatus, string> = {
  green:  "Compliant",
  yellow: "Expiring soon",
  red:    "Non-compliant",
  black:  "Stolen / seized / wanted",
};

export const statusColor: Record<VehicleStatus, string> = {
  green:  "#16a34a",
  yellow: "#ca8a04",
  red:    "#dc2626",
  black:  "#0f172a",
};

export function statusBadgeClasses(s: VehicleStatus) {
  switch (s) {
    case "green":  return "bg-emerald-100 text-emerald-900 ring-emerald-300";
    case "yellow": return "bg-amber-100   text-amber-900   ring-amber-300";
    case "red":    return "bg-red-100     text-red-900     ring-red-300";
    case "black":  return "bg-slate-900   text-white       ring-slate-700";
  }
}
