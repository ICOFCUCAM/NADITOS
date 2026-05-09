"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Button, Card, EmptyState, Field, IconButton, Input, KeyValue, Plate,
  Pill, services, statusLabel, useSession, type VehicleStatus,
} from "@naditos/web-common";

// Scan workspace.
//
// The screen is split into a camera viewport (with reticle), a big
// plate readout, and an "evidence drawer" of compliance + suggested
// actions. The camera lifecycle stays in this component so the
// officer never has to manually start/stop. All taps are ≥56px
// (size="field") because field use is one-handed in gloves.

type Vehicle = {
  id: string;
  plate: string;
  make?: string;
  model?: string;
  year?: number;
  status: VehicleStatus;
  insurance_expires_at?: string | null;
  inspection_expires_at?: string | null;
  is_stolen: boolean;
  is_seized: boolean;
  is_wanted: boolean;
};

const STATUS_RING: Record<VehicleStatus, string> = {
  green:  "ring-[var(--c-ok-500)]   shadow-[0_0_0_1px_rgba(16,185,129,0.30),0_0_60px_rgba(16,185,129,0.18)]",
  yellow: "ring-[var(--c-warn-500)] shadow-[0_0_0_1px_rgba(245,158,11,0.30),0_0_60px_rgba(245,158,11,0.18)]",
  red:    "ring-[var(--c-bad-500)]  shadow-[0_0_0_1px_rgba(239,68,68,0.40),0_0_60px_rgba(239,68,68,0.20)]",
  black:  "ring-black                shadow-[0_0_0_1px_rgba(0,0,0,0.60),0_0_60px_rgba(0,0,0,0.45)]",
};

export default function ScanPage() {
  const { session } = useSession();
  const router = useRouter();
  const [plate, setPlate] = useState("");
  const [vehicle, setVehicle] = useState<Vehicle | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);
  const [busy, setBusy] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

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
        setErr("Camera unavailable: " + (e?.message ?? "permission denied"));
        setScanning(false);
      }
    })();
    return () => { stream?.getTracks().forEach((t) => t.stop()); };
  }, [scanning]);

  async function captureAndLookup() {
    if (!videoRef.current || !canvasRef.current || !session) return;
    const v = videoRef.current;
    const c = canvasRef.current;
    c.width = v.videoWidth || 640;
    c.height = v.videoHeight || 480;
    c.getContext("2d")!.drawImage(v, 0, 0, c.width, c.height);
    setScanning(false);
    if (plate.trim()) await lookup(plate.trim().toUpperCase());
  }

  async function lookup(p: string) {
    if (!session) return;
    setBusy(true); setErr(null);
    try {
      const v = await services.registry(`/v1/vehicles/by-plate/${encodeURIComponent(p)}`, {
        token: session.accessToken, tenant: session.user.tenant,
      });
      setVehicle(v as Vehicle);
    } catch (e: any) {
      setVehicle(null);
      setErr(e?.message ?? "Lookup failed");
    } finally { setBusy(false); }
  }

  const offences = vehicle ? suggestedOffences(vehicle) : [];
  const flagged = vehicle && (vehicle.is_stolen || vehicle.is_seized || vehicle.is_wanted);

  return (
    <div className="px-4 pt-4 space-y-4">
      {/* ── Camera viewport ─────────────────────────────────────────── */}
      <div
        className={
          "relative rounded-[var(--r-xl)] overflow-hidden ring-1 " +
          (vehicle ? STATUS_RING[vehicle.status]
                   : "ring-[var(--border-default)]")
        }
      >
        <div className="aspect-[4/3] bg-black grid place-items-center relative">
          {scanning ? (
            <video ref={videoRef} className="w-full h-full object-cover"
                   muted playsInline />
          ) : (
            <div className="text-[var(--fg-muted)] text-xs uppercase tracking-[0.18em] flex items-center gap-2">
              <span className="h-2 w-2 rounded-full bg-[var(--fg-muted)]" /> Camera offline
            </div>
          )}
          <canvas ref={canvasRef} className="hidden" />

          {/* Reticle */}
          <div aria-hidden className="absolute inset-6 pointer-events-none">
            <div className="absolute inset-0 rounded-[var(--r-lg)]
                            ring-1 ring-[var(--accent-primary)]/30" />
            {Corners.map((c, i) => (
              <span key={i} className={`absolute h-5 w-5 ${c}
                                        border-[var(--accent-primary)]`} />
            ))}
            {scanning && (
              <div className="absolute left-0 right-0 h-px bg-[var(--accent-primary)]/60
                              animate-[scanline_2.4s_ease-in-out_infinite]"
                   style={{ top: "50%" }} />
            )}
          </div>

          {/* Top-right HUD */}
          <div className="absolute top-3 right-3 flex items-center gap-2">
            <span className="rounded-[var(--r-pill)] bg-black/40 ring-1 ring-white/10
                             px-2 py-0.5 text-[10px] uppercase tracking-[0.18em] text-white/80">
              {scanning ? "Live · Rear" : "Standby"}
            </span>
          </div>
        </div>

        {/* Camera controls */}
        <div className="grid grid-cols-2 gap-2 p-3 bg-[var(--bg-surface)]/80 backdrop-blur-md">
          <Button size="field" tone={scanning ? "danger" : "primary"}
            onClick={() => setScanning((s) => !s)} fullWidth>
            {scanning ? "Stop camera" : "Start camera"}
          </Button>
          <Button size="field" tone="secondary"
            onClick={captureAndLookup} disabled={!scanning} fullWidth>
            Capture & lookup
          </Button>
        </div>
      </div>

      {/* ── Manual plate entry ──────────────────────────────────────── */}
      <Card pad="sm" tone="elevated">
        <Field label="Plate">
          <div className="flex gap-2">
            <Input value={plate}
              onChange={(e) => setPlate(e.target.value.toUpperCase())}
              inputSize="lg"
              placeholder="AB-12-CD"
              autoCapitalize="characters"
              autoCorrect="off"
              spellCheck={false}
              className="font-mono tracking-[0.18em] uppercase"
              style={{ fontFamily: "var(--ff-mono)" }}
            />
            <IconButton label="Lookup plate" tone="primary" size="lg"
              disabled={!plate.trim() || busy}
              onClick={() => lookup(plate.trim().toUpperCase())}>
              <ArrowRight />
            </IconButton>
          </div>
        </Field>
        {err && <p className="mt-2 text-sm text-[var(--c-bad-300)]">{err}</p>}
      </Card>

      {/* ── Result ──────────────────────────────────────────────────── */}
      {!vehicle && !busy && (
        <Card pad="md" tone="outline">
          <EmptyState
            title="Awaiting a plate"
            description="Use the camera or type the plate. The vehicle's compliance state, alerts, and recommended action will appear here."
          />
        </Card>
      )}

      {vehicle && (
        <Card pad="md"
          tone={flagged ? "alert" : "elevated"}
          className={flagged ? "" : ""}>
          {/* Plate + status row */}
          <div className="flex items-center justify-between gap-3">
            <Plate value={vehicle.plate} size="xl" />
            <span className={
              "inline-flex items-center gap-2 rounded-[var(--r-pill)] " +
              "px-3 py-1 text-[11px] uppercase tracking-[0.14em] font-medium ring-1 " +
              (vehicle.status === "green"  ? "bg-[var(--status-ok-bg)]   text-[var(--status-ok-fg)]   ring-[var(--c-ok-500)]/40" :
               vehicle.status === "yellow" ? "bg-[var(--status-warn-bg)] text-[var(--status-warn-fg)] ring-[var(--c-warn-500)]/40" :
               vehicle.status === "red"    ? "bg-[var(--status-bad-bg)]  text-[var(--status-bad-fg)]  ring-[var(--c-bad-500)]/40" :
                                              "bg-black text-white ring-white/40")
            }>
              <span className="h-2 w-2 rounded-full" style={{
                background:
                  vehicle.status === "green"  ? "var(--c-ok-500)" :
                  vehicle.status === "yellow" ? "var(--c-warn-500)" :
                  vehicle.status === "red"    ? "var(--c-bad-500)" :
                                                 "white",
              }} />
              {statusLabel[vehicle.status]}
            </span>
          </div>

          <div className="mt-3 text-sm text-[var(--fg-secondary)]">
            {[vehicle.make, vehicle.model, vehicle.year].filter(Boolean).join(" ") || "—"}
          </div>

          {/* Compliance grid */}
          <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3">
            <KeyValue k="Insurance" v={fmt(vehicle.insurance_expires_at)}
              tone={isExpired(vehicle.insurance_expires_at) ? "default" : "default"} />
            <KeyValue k="Inspection" v={fmt(vehicle.inspection_expires_at)} />
          </div>

          {/* Alerts */}
          {flagged && (
            <div className="mt-4 rounded-[var(--r-md)] bg-black text-white
                            px-3 py-3 flex items-start gap-3 ring-1 ring-white/20">
              <span aria-hidden className="text-xl leading-none">⚠</span>
              <div className="space-y-1">
                <div className="text-[11px] uppercase tracking-[0.18em] text-white/70">
                  Alert list
                </div>
                <div className="text-base font-semibold tracking-wide">
                  {vehicle.is_stolen && <>STOLEN </>}
                  {vehicle.is_seized && <>SEIZED </>}
                  {vehicle.is_wanted && <>WANTED</>}
                </div>
                <div className="text-xs text-white/70">
                  Detain vehicle and call dispatch immediately.
                </div>
              </div>
            </div>
          )}

          {/* Suggested offences */}
          {offences.length > 0 && (
            <div className="mt-4">
              <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-2">
                Suggested charges
              </div>
              <div className="space-y-2">
                {offences.map((o) => (
                  <button
                    key={o.code}
                    onClick={() => router.push(
                      `/fine?plate=${encodeURIComponent(vehicle.plate)}&vid=${vehicle.id}&offence=${o.code}`,
                    )}
                    className="w-full text-left rounded-[var(--r-md)] px-4 py-3
                               bg-[var(--status-warn-bg)] ring-1 ring-[var(--c-warn-500)]/30
                               hover:ring-[var(--c-warn-500)]/60
                               focus-visible:outline-none focus-visible:[box-shadow:var(--focus-ring)]
                               transition-[box-shadow] duration-[var(--m-fast)]"
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="text-[11px] uppercase tracking-[0.16em] font-medium text-[var(--status-warn-fg)]">
                          {o.code}
                        </div>
                        <div className="text-sm text-[var(--fg-primary)]">{o.label}</div>
                      </div>
                      <ArrowRight />
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="mt-4 grid grid-cols-2 gap-2">
            <Button size="field" tone="secondary"
              onClick={() => router.push(`/verify?plate=${encodeURIComponent(vehicle.plate)}`)}>
              Verify driver
            </Button>
            <Button size="field" tone="danger"
              onClick={() => router.push(
                `/fine?plate=${encodeURIComponent(vehicle.plate)}&vid=${vehicle.id}`,
              )}>
              Issue fine
            </Button>
          </div>
        </Card>
      )}

      {/* CSS keyframe for scanline */}
      <style jsx global>{`
        @keyframes scanline {
          0%   { transform: translateY(-50%); opacity: 0.0; }
          15%  { opacity: 1.0; }
          100% { transform: translateY(50%); opacity: 0.0; }
        }
      `}</style>
    </div>
  );
}

const Corners = [
  "top-0 left-0 border-t-2 border-l-2 rounded-tl-[var(--r-md)]",
  "top-0 right-0 border-t-2 border-r-2 rounded-tr-[var(--r-md)]",
  "bottom-0 left-0 border-b-2 border-l-2 rounded-bl-[var(--r-md)]",
  "bottom-0 right-0 border-b-2 border-r-2 rounded-br-[var(--r-md)]",
];

function fmt(s?: string | null) {
  if (!s) return <span className="text-[var(--c-bad-300)] font-medium">not on file</span>;
  const exp = new Date(s);
  const days = Math.floor((exp.getTime() - Date.now()) / 86400_000);
  const date = exp.toISOString().slice(0, 10);
  if (days < 0) return <span className="text-[var(--c-bad-300)] font-medium">{date} (expired {Math.abs(days)}d)</span>;
  if (days <= 30) return <span className="text-[var(--c-warn-300)] font-medium">{date} ({days}d left)</span>;
  return <span className="text-[var(--c-ok-300)] font-medium">{date} ({days}d left)</span>;
}
function isExpired(s?: string | null) {
  return !s || new Date(s).getTime() < Date.now();
}

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

function ArrowRight() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
         strokeLinecap="round" strokeLinejoin="round" className="h-5 w-5">
      <path d="M5 12h14"/>
      <path d="m13 6 6 6-6 6"/>
    </svg>
  );
}
