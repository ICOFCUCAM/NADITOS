"use client";

import Link from "next/link";
import { Card } from "@naditos/web-common";

export default function CitizenHome() {
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
          <div className="text-lg font-semibold">My vehicles</div>
          <div className="text-sm text-slate-600">
            Vehicles registered to you with live insurance and inspection status.
          </div>
        </Card></Link>
        <Link href="/fines"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">My fines</div>
          <div className="text-sm text-slate-600">
            View, dispute, or pay outstanding fines.
          </div>
        </Card></Link>
        <Link href="/license"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">My driver license</div>
          <div className="text-sm text-slate-600">
            License standing, demerit points, and suspension history.
          </div>
        </Card></Link>
        <Link href="/inbox"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">Notifications</div>
          <div className="text-sm text-slate-600">
            Messages we've sent to your registered email or phone.
          </div>
        </Card></Link>
        <Link href="/transfers"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">Ownership transfers</div>
          <div className="text-sm text-slate-600">
            Hand a vehicle over to a buyer, or accept one with a code.
          </div>
        </Card></Link>
      </div>
    </>
  );
}
