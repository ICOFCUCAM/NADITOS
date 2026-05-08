"use client";

import Link from "next/link";
import { Card } from "@naditos/web-common";

export default function CitizenHome() {
  return (
    <>
      <h1 className="text-2xl font-bold">My account</h1>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Link href="/fines"><Card className="hover:shadow-md transition">
          <div className="text-lg font-semibold">My fines</div>
          <div className="text-sm text-slate-600">View, dispute, or pay outstanding fines.</div>
        </Card></Link>
        <Card>
          <div className="text-lg font-semibold">My vehicles</div>
          <div className="text-sm text-slate-600">
            Phase-2: list of vehicles you own with insurance / inspection status.
          </div>
        </Card>
      </div>
    </>
  );
}
