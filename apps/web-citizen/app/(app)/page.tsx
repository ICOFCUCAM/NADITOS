"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Card, Pill, services, useSession } from "@naditos/web-common";

// Dashboard counts pulled in parallel after sign-in. Each is best-
// effort — a 404 (e.g. citizen has no license on file) just hides
// the badge rather than crashing the dashboard.
type Counts = {
  vehicles: number;
  fines_open: number;
  has_license: boolean;
  license_points: number;
  unread_notifications: number;
  pending_transfers: number;
};

export default function CitizenHome() {
  const { session } = useSession();
  const [c, setC] = useState<Partial<Counts>>({});

  useEffect(() => {
    if (!session) return;
    const opts = { token: session.accessToken, tenant: session.user.tenant };

    services.registry("/v1/citizens/me/vehicles", opts)
      .then((r: any) => setC((s) => ({ ...s, vehicles: (r.items ?? []).length })))
      .catch(() => {});
    services.fines("/v1/fines/mine", opts)
      .then((r: any) => setC((s) => ({
        ...s,
        fines_open: (r.items ?? []).filter((f: any) =>
          ["issued", "warned", "overdue", "escalated"].includes(f.status)).length,
      })))
      .catch(() => {});
    services.license("/v1/citizens/me/license", opts)
      .then((r: any) => setC((s) => ({
        ...s, has_license: true, license_points: r.license?.points ?? 0,
      })))
      .catch(() => setC((s) => ({ ...s, has_license: false })));
    services.notify("/v1/citizens/me/notifications", opts)
      .then((r: any) => setC((s) => ({ ...s, unread_notifications: (r.items ?? []).length })))
      .catch(() => {});
    services.registry("/v1/citizens/me/transfers", opts)
      .then((r: any) => setC((s) => ({
        ...s,
        pending_transfers: (r.items ?? []).filter((t: any) => t.status === "pending").length,
      })))
      .catch(() => {});
  }, [session]);

  return (
    <>
      <h1 className="text-2xl font-bold">My account</h1>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Link href="/owner"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">My profile</div>
          <div className="text-sm text-slate-600">
            Claim or update the owner record we use to send fine notices and reminders.
          </div>
        </Card></Link>

        <Link href="/vehicles"><Card className="hover:shadow-md transition">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-lg font-semibold">My vehicles</div>
              <div className="text-sm text-slate-600">
                Vehicles registered to you with live insurance and inspection status.
              </div>
            </div>
            {c.vehicles !== undefined && (
              <Pill tone="slate">{c.vehicles}</Pill>
            )}
          </div>
        </Card></Link>

        <Link href="/fines"><Card className="hover:shadow-md transition">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-lg font-semibold">My fines</div>
              <div className="text-sm text-slate-600">
                View, dispute, or pay outstanding fines.
              </div>
            </div>
            {c.fines_open !== undefined && c.fines_open > 0 && (
              <Pill tone="amber">{c.fines_open} open</Pill>
            )}
            {c.fines_open === 0 && <Pill tone="green">none open</Pill>}
          </div>
        </Card></Link>

        <Link href="/license"><Card className="hover:shadow-md transition">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-lg font-semibold">My driver license</div>
              <div className="text-sm text-slate-600">
                License standing, demerit points, and suspension history.
              </div>
            </div>
            {c.has_license && (
              <Pill tone={(c.license_points ?? 0) >= 9 ? "red" :
                          (c.license_points ?? 0) >= 6 ? "amber" : "green"}>
                {c.license_points ?? 0} pts
              </Pill>
            )}
          </div>
        </Card></Link>

        <Link href="/inbox"><Card className="hover:shadow-md transition">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-lg font-semibold">Notifications</div>
              <div className="text-sm text-slate-600">
                Messages we've sent to your registered email or phone.
              </div>
            </div>
            {c.unread_notifications !== undefined && c.unread_notifications > 0 && (
              <Pill tone="slate">{c.unread_notifications}</Pill>
            )}
          </div>
        </Card></Link>

        <Link href="/transfers"><Card className="hover:shadow-md transition">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-lg font-semibold">Ownership transfers</div>
              <div className="text-sm text-slate-600">
                Hand a vehicle over to a buyer, or accept one with a code.
              </div>
            </div>
            {c.pending_transfers !== undefined && c.pending_transfers > 0 && (
              <Pill tone="amber">{c.pending_transfers} pending</Pill>
            )}
          </div>
        </Card></Link>
      </div>
    </>
  );
}
