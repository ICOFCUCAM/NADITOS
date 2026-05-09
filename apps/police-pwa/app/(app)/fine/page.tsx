"use client";

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Button, Card, Field, Input, KeyValue, Mono, Pill, Plate,
  SectionHeader, services, useSession,
} from "@naditos/web-common";

// Issue-fine workflow.
//
// Anti-corruption rules enforced client-side as guard rails (server
// re-checks, never trusts the client):
//   • photo evidence required, hashed in-browser (SHA-256)
//   • GPS coordinates required, accuracy reported
//   • amount is server-priced — officer never picks
//   • offence code chosen from a closed set bound to the country pack

const OFFENCES = [
  { code: "INS_EXPIRED",    label: "Driving without valid insurance" },
  { code: "INSP_EXPIRED",   label: "Driving without valid roadworthiness inspection" },
  { code: "REG_EXPIRED",    label: "Expired registration" },
  { code: "TAX_UNPAID",     label: "Unpaid vehicle tax" },
  { code: "PLATE_OBSCURED", label: "Obscured / illegible plate" },
  { code: "SEAT_BELT",      label: "No seat belt" },
  { code: "MOBILE_PHONE",   label: "Mobile phone use while driving" },
  { code: "RED_LIGHT",      label: "Running red light" },
  { code: "SPEED_30",       label: "Speeding 30+ km/h over limit" },
];

export default function FinePage() {
  const { session } = useSession();
  const sp = useSearchParams();
  const router = useRouter();
  const [plate, setPlate] = useState(sp.get("plate") ?? "");
  const [offence, setOffence] = useState(
    OFFENCES.some((o) => o.code === sp.get("offence"))
      ? (sp.get("offence") as string)
      : OFFENCES[0].code,
  );
  const [photoSha, setPhotoSha] = useState<string | null>(null);
  const [photoBytes, setPhotoBytes] = useState<number>(0);
  const [photoName, setPhotoName] = useState<string | null>(null);
  const [geo, setGeo] = useState<{ lat: number; lng: number; acc: number } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<string | null>(null);

  useEffect(() => {
    if (!navigator.geolocation) return;
    navigator.geolocation.getCurrentPosition(
      (p) => setGeo({ lat: p.coords.latitude, lng: p.coords.longitude, acc: p.coords.accuracy }),
      () => setGeo(null),
      { enableHighAccuracy: true, timeout: 8000 },
    );
  }, []);

  async function onPhoto(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (!f) return;
    const buf = await f.arrayBuffer();
    const hash = await crypto.subtle.digest("SHA-256", buf);
    const hex = Array.from(new Uint8Array(hash))
      .map((b) => b.toString(16).padStart(2, "0")).join("");
    setPhotoSha(hex);
    setPhotoBytes(f.size);
    setPhotoName(f.name);
  }

  async function submit() {
    if (!session) return;
    if (!photoSha) {
      setErr("Photo evidence is required (anti-corruption rule).");
      return;
    }
    if (!geo) {
      setErr("GPS coordinates are required.");
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      const s3Key = `evidence/${session.user.tenant}/${Date.now()}-${photoSha.slice(0, 8)}.jpg`;
      const r = await services.fines("/v1/fines", {
        method: "POST",
        token: session.accessToken,
        tenant: session.user.tenant,
        body: {
          plate,
          offence_code: offence,
          geo_lat: geo.lat,
          geo_lng: geo.lng,
          geo_accuracy_m: geo.acc,
          device_id: deviceID(),
          evidence: [{
            kind: "photo",
            s3_key: s3Key,
            sha256: photoSha,
            bytes: photoBytes,
            taken_at: new Date().toISOString(),
          }],
        },
      });
      setDone((r as any).id);
    } catch (e: any) {
      setErr(e?.message ?? "Failed to issue fine");
    } finally {
      setBusy(false);
    }
  }

  if (done) {
    return (
      <div className="px-4 pt-6 space-y-4">
        <div className="rounded-[var(--r-xl)] p-6 ring-1
                        ring-[var(--c-ok-500)] bg-[var(--status-ok-bg)] text-center">
          <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--status-ok-fg)]">
            Fine issued
          </div>
          <div className="mt-2 text-3xl font-semibold tracking-[0.04em]"
               style={{ fontFamily: "var(--ff-display)" }}>
            <Mono>{done.slice(0,8)}</Mono>
          </div>
          <div className="mt-3 text-sm text-[var(--fg-secondary)]">
            Sealed in the audit chain. The motorist will be notified.
          </div>
        </div>
        <Button fullWidth size="field" tone="primary"
          onClick={() => router.replace("/scan")}>
          Back to scan
        </Button>
        <Button fullWidth size="field" tone="secondary"
          onClick={() => router.replace(`/fine/${done}`)}>
          View fine detail
        </Button>
      </div>
    );
  }

  return (
    <div className="px-4 pt-4 space-y-4">
      <SectionHeader
        eyebrow="Field workflow"
        title="Issue fine"
        description="Server prices the offence. Photo + GPS are gates,
                     not optional — the receipt won't seal without them." />

      <Card pad="md" tone="elevated">
        <Field label="Plate">
          <Input value={plate}
            onChange={(e) => setPlate(e.target.value.toUpperCase())}
            inputSize="lg"
            autoCapitalize="characters" autoCorrect="off" spellCheck={false}
            className="font-mono tracking-[0.18em] uppercase"
            style={{ fontFamily: "var(--ff-mono)" }} />
        </Field>
      </Card>

      <Card pad="md" tone="elevated">
        <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-2">
          Offence
        </div>
        <div className="space-y-1.5">
          {OFFENCES.map((o) => (
            <label key={o.code}
              className={
                "flex items-center justify-between gap-3 rounded-[var(--r-md)] " +
                "px-3 py-2.5 cursor-pointer ring-1 transition-[box-shadow] " +
                (offence === o.code
                  ? "bg-[var(--accent-soft-bg)] ring-[var(--accent-primary)]/40"
                  : "ring-[var(--border-default)] hover:ring-[var(--border-strong)]")
              }
            >
              <div>
                <div className="text-[11px] uppercase tracking-[0.16em] text-[var(--fg-muted)] font-mono">
                  {o.code}
                </div>
                <div className="text-sm">{o.label}</div>
              </div>
              <input
                type="radio" name="offence" value={o.code}
                checked={offence === o.code}
                onChange={() => setOffence(o.code)}
                className="h-4 w-4 accent-[var(--accent-primary)]"
              />
            </label>
          ))}
        </div>
      </Card>

      <Card pad="md" tone="elevated">
        <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-2">
          Evidence (required)
        </div>
        <label className="block">
          <input type="file" accept="image/*" capture="environment" onChange={onPhoto}
            className="block w-full text-sm file:mr-3 file:rounded-[var(--r-md)] file:border-0
                       file:bg-[var(--accent-primary)] file:text-[var(--accent-primary-fg)]
                       file:px-4 file:py-2 file:font-medium
                       file:cursor-pointer text-[var(--fg-secondary)]" />
        </label>
        {photoSha && (
          <div className="mt-3 grid grid-cols-2 gap-3">
            <KeyValue k="File" v={photoName ?? "—"} />
            <KeyValue k="Size" v={`${Math.round(photoBytes/1024)} KB`} />
            <KeyValue k="SHA-256 (hashed in-browser)" mono v={photoSha.slice(0, 24) + "…"} />
          </div>
        )}
      </Card>

      <Card pad="md" tone="elevated">
        <div className="text-[11px] uppercase tracking-[0.18em] text-[var(--fg-muted)] mb-2">
          Telemetry
        </div>
        <div className="grid grid-cols-2 gap-3">
          <KeyValue k="GPS" v={geo
            ? <>{geo.lat.toFixed(5)}, {geo.lng.toFixed(5)} <span className="text-[var(--fg-muted)]">±{Math.round(geo.acc)}m</span></>
            : "acquiring…"} mono />
          <KeyValue k="Device" mono v={deviceID().slice(0, 14) + "…"} />
        </div>
        {!geo && (
          <div className="mt-2"><Pill tone="amber">Awaiting GPS</Pill></div>
        )}
      </Card>

      {err && <Card tone="alert" pad="md">{err}</Card>}

      <div className="sticky bottom-0 -mx-4 px-4 py-3
                      bg-[var(--bg-canvas)]/90 backdrop-blur-md
                      border-t border-[var(--border-subtle)]">
        <Button onClick={submit} disabled={busy || !photoSha || !geo}
          tone="danger" size="field" fullWidth>
          {busy ? "Issuing…" : "Seal & issue"}
        </Button>
        <p className="mt-2 text-[11px] text-[var(--fg-muted)] tracking-wide text-center">
          Amount priced by the regulation engine for{" "}
          <span className="font-mono">{offence}</span>. Cannot be overridden.
        </p>
      </div>
    </div>
  );
}

function deviceID(): string {
  if (typeof window === "undefined") return "server";
  const k = "naditos.device_id";
  let id = window.localStorage.getItem(k);
  if (!id) {
    id = "dev-" + crypto.randomUUID();
    window.localStorage.setItem(k, id);
  }
  return id;
}
