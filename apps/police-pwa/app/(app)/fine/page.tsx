"use client";

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Input, services, useSession } from "@naditos/web-common";

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
  const [offence, setOffence] = useState(OFFENCES[0].code);
  const [photoB64, setPhotoB64] = useState<string | null>(null);
  const [photoSha, setPhotoSha] = useState<string | null>(null);
  const [photoBytes, setPhotoBytes] = useState<number>(0);
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
    const b64 = btoa(String.fromCharCode(...new Uint8Array(buf).slice(0, 1)));
    setPhotoB64(b64);
    setPhotoSha(hex);
    setPhotoBytes(f.size);
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
      // In production the photo is uploaded directly to S3 via a presigned
      // URL and the s3_key passed here. For Phase-1 the key is a stub.
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
      <div className="p-6 space-y-4">
        <div className="rounded-lg bg-emerald-900/40 border border-emerald-700 p-4 text-emerald-100">
          Fine issued. Reference: <span className="font-mono">{done.slice(0,8)}</span>
        </div>
        <Button className="w-full" onClick={() => router.replace("/scan")}>Back to scan</Button>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-4">
      <h1 className="text-xl font-bold">Issue fine</h1>

      <label className="block text-xs uppercase tracking-wide text-slate-400">Plate</label>
      <Input value={plate} onChange={(e) => setPlate(e.target.value.toUpperCase())}
        className="bg-slate-800 border-slate-700 text-white font-mono uppercase" />

      <label className="block text-xs uppercase tracking-wide text-slate-400">Offence</label>
      <select value={offence} onChange={(e) => setOffence(e.target.value)}
        className="block w-full rounded-md bg-slate-800 border-slate-700 text-white px-3 py-2 text-sm">
        {OFFENCES.map((o) => <option key={o.code} value={o.code}>{o.label}</option>)}
      </select>

      <label className="block text-xs uppercase tracking-wide text-slate-400">Photo evidence (required)</label>
      <input type="file" accept="image/*" capture="environment" onChange={onPhoto}
        className="block w-full text-sm" />
      {photoSha && <p className="text-xs text-slate-400">sha256: {photoSha.slice(0,16)}…</p>}

      <div className="text-xs text-slate-400">
        GPS: {geo ? `${geo.lat.toFixed(5)}, ${geo.lng.toFixed(5)} (±${Math.round(geo.acc)}m)` : "acquiring…"}
      </div>
      <div className="text-xs text-slate-400">
        Device: <span className="font-mono">{deviceID()}</span>
      </div>

      {err && <p className="text-sm text-red-400">{err}</p>}

      <Button onClick={submit} disabled={busy || !photoSha || !geo}
        className="w-full bg-red-600 hover:bg-red-700">
        {busy ? "Issuing…" : "Issue fine (server-priced)"}
      </Button>
      <p className="text-xs text-slate-500">
        Amount is determined by the regulation engine for offence{" "}
        <span className="font-mono">{offence}</span>. Officers cannot set the amount.
      </p>
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
