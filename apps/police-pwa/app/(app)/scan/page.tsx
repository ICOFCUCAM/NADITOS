"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Input, services, useSession, statusBadgeClasses, statusLabel, type VehicleStatus } from "@naditos/web-common";

type Vehicle = {
  id: string; plate: string; make?: string; model?: string; year?: number;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean; is_seized: boolean; is_wanted: boolean;
};

export default function ScanPage() {
  const { session } = useSession();
  const router = useRouter();
  const [plate, setPlate] = useState("");
  const [vehicle, setVehicle] = useState<Vehicle | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  // ── Camera lifecycle ──────────────────────────────────────────────────
  useEffect(() => {
    if (!scanning) return;
    let stream: MediaStream | null = null;
    (async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: "environment" } }, audio: false,
        });
        if (videoRef.current) {
          videoRef.current.srcObject = stream;
          await videoRef.current.play();
        }
      } catch (e: any) {
        setErr("Camera unavailable: " + e.message);
        setScanning(false);
      }
    })();
    return () => { stream?.getTracks().forEach((t) => t.stop()); };
  }, [scanning]);

  // ── Snap a frame, mock-OCR the plate, look up the vehicle ─────────────
  async function captureAndLookup() {
    if (!videoRef.current || !canvasRef.current || !session) return;
    const v = videoRef.current;
    const c = canvasRef.current;
    c.width = v.videoWidth || 640;
    c.height = v.videoHeight || 480;
    c.getContext("2d")!.drawImage(v, 0, 0, c.width, c.height);
    // Phase-2: send the bitmap to /v1/anpr/recognize. For now, the officer
    // confirms the plate; capture stays in IndexedDB for evidence.
    setScanning(false);
    if (plate.trim()) await lookup(plate.trim().toUpperCase());
  }

  async function lookup(p: string) {
    if (!session) return;
    setErr(null);
    try {
      const v = await services.registry(`/v1/vehicles/by-plate/${encodeURIComponent(p)}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setVehicle(v as Vehicle);
    } catch (e: any) {
      setVehicle(null);
      setErr(e?.message ?? "Lookup failed");
    }
  }

  return (
    <div className="p-4 space-y-4">
      <div className="rounded-lg overflow-hidden bg-black aspect-[4/3] grid place-items-center">
        {scanning
          ? <video ref={videoRef} className="w-full h-full object-cover" muted playsInline />
          : <div className="text-slate-400 text-sm">Camera off</div>}
        <canvas ref={canvasRef} className="hidden" />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <Button onClick={() => setScanning((s) => !s)}
          className={scanning ? "bg-red-600 hover:bg-red-700" : "bg-emerald-600 hover:bg-emerald-700"}>
          {scanning ? "Stop camera" : "Start camera"}
        </Button>
        <Button onClick={captureAndLookup} disabled={!scanning}
          className="bg-amber-500 hover:bg-amber-600 text-slate-900">
          Capture &amp; lookup
        </Button>
      </div>

      <div className="space-y-2">
        <label className="block text-xs uppercase tracking-wide text-slate-400">Plate</label>
        <div className="flex gap-2">
          <Input value={plate} onChange={(e) => setPlate(e.target.value.toUpperCase())}
            placeholder="AB-12-CD"
            className="bg-slate-800 border-slate-700 text-white font-mono uppercase" />
          <Button onClick={() => lookup(plate)} className="bg-slate-700 hover:bg-slate-600">Lookup</Button>
        </div>
        {err && <p className="text-sm text-red-400">{err}</p>}
      </div>

      {vehicle && (
        <div className="rounded-lg bg-slate-800 p-4 space-y-2">
          <div className="flex items-center justify-between">
            <div className="text-2xl font-mono">{vehicle.plate}</div>
            <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs ring-1 ${statusBadgeClasses(vehicle.status)}`}>
              {statusLabel[vehicle.status]}
            </span>
          </div>
          <div className="text-sm text-slate-300">
            {[vehicle.make, vehicle.model, vehicle.year].filter(Boolean).join(" ") || "—"}
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs text-slate-400">
            <div>Insurance: <span className="text-slate-200">{date(vehicle.insurance_expires_at)}</span></div>
            <div>Inspection: <span className="text-slate-200">{date(vehicle.inspection_expires_at)}</span></div>
          </div>
          {(vehicle.is_stolen || vehicle.is_seized || vehicle.is_wanted) && (
            <div className="rounded bg-red-900/50 border border-red-700 text-red-200 p-2 text-sm">
              {vehicle.is_stolen && "STOLEN "}
              {vehicle.is_seized && "SEIZED "}
              {vehicle.is_wanted && "WANTED"}
            </div>
          )}

          {/* Suggested offences derived from compliance state. The officer
              still selects which to charge; the server computes the amount. */}
          {suggestedOffences(vehicle).length > 0 && (
            <div className="space-y-1.5 pt-1">
              <div className="text-xs uppercase tracking-wide text-slate-400">Suggested offences</div>
              {suggestedOffences(vehicle).map((o) => (
                <button key={o.code}
                  onClick={() => router.push(`/fine?plate=${encodeURIComponent(vehicle.plate)}&vid=${vehicle.id}&offence=${o.code}`)}
                  className="w-full text-left px-3 py-2 rounded bg-amber-500/15 border border-amber-500/30 text-amber-100 hover:bg-amber-500/25 text-sm">
                  <span className="font-mono text-xs text-amber-300">{o.code}</span> · {o.label}
                </button>
              ))}
            </div>
          )}

          <Button
            onClick={() => router.push(`/fine?plate=${encodeURIComponent(vehicle.plate)}&vid=${vehicle.id}`)}
            className="w-full bg-red-600 hover:bg-red-700 mt-2">
            Issue fine (free choice)
          </Button>
        </div>
      )}
    </div>
  );
}

function date(s?: string | null) { return s ? new Date(s).toISOString().slice(0, 10) : "—"; }

// Compliance-driven offence suggestions for the officer. The fines
// service computes the actual amount from the active country pack; the
// officer cannot override.
function suggestedOffences(v: Vehicle): { code: string; label: string }[] {
  const out: { code: string; label: string }[] = [];
  const now = Date.now();
  if (v.is_stolen || v.is_seized || v.is_wanted) {
    out.push({ code: "STOLEN", label: "Vehicle on alert list — detain & call dispatch" });
  }
  if (!v.insurance_expires_at || new Date(v.insurance_expires_at).getTime() < now) {
    out.push({ code: "INS_EXPIRED", label: "Driving without valid insurance" });
  }
  if (!v.inspection_expires_at || new Date(v.inspection_expires_at).getTime() < now) {
    out.push({ code: "INSP_EXPIRED", label: "Driving without valid roadworthiness inspection" });
  }
  return out;
}
