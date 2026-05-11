# NADITOS Intelligent Enforcement Camera Platform — Architecture

> Sovereign-grade roadside enforcement camera platform.
> **Chosen path: PWA substrate + native companion plan.**
> The Police PWA carries the evidence, audit, offline, and overlay
> substrate. A dedicated native (iOS/Android) companion app — or rugged
> checkpoint device — carries the real perception layer (long-range
> capture, advanced enhancement, high-FPS on-device inference). Both
> sources flow into the same evidence chain and the same ANPR backend.

## 1. Why this split

A web PWA in a mobile browser has hard ceilings that a sovereign-grade
enforcement camera cannot accept on its own:

- `getUserMedia` exposes basic constraints (resolution, frame rate,
  facing-mode, sometimes `zoom` / `torch` / `focusMode`), but iOS Safari
  in particular gates the richest controls. **No RAW capture, no
  manual sensor control, no neural-engine acceleration.**
- On-device ML in-browser runs on TF.js / ONNX Web with WebGL or
  WebGPU. Practical inference budget on mid-range mobile: **5–15 FPS
  for a YOLO-class detector**, considerably less for OCR. Battery
  drain is real.
- No background sensor access. The tab can be killed at any time.
- Optical zoom, image-stabilisation, and computational night-mode are
  vendor pipelines we don't get inside a `<video>` element.

A PWA-only "intelligent enforcement camera" that promises long-distance
zoom, deblurring, and real-time AI overlay is a toy that looks tactical
in screenshots and fails on the roadside. We split the responsibilities:

- **PWA** → ubiquitous, low-friction officer device. Owns evidence
  capture metadata, offline queue, hotlist overlay, lightweight
  *assistive* perception. Authoritative for chain-of-custody.
- **Native companion** (Phase-N) → either an officer-owned phone app
  or a fixed rugged device. Owns long-range optics control, sustained
  high-FPS pipelines, edge-AI detection, low-light/IR. Streams frames +
  metadata into the same evidence chain the PWA writes to.
- **Both** are clients of the same ANPR gateway + evidence API. The
  backend is the source of truth for plate recognition; the device
  *assists* with hints and overlays.

This split also matches the existing `anpr_scans.source` enum
(`officer | fixed_cam | toll | border` — and we will extend it). Multi-
source was always the design.

## 2. Capability matrix (PWA vs. Native vs. Backend)

| Capability | PWA | Native companion | Backend authoritative |
|---|---|---|---|
| Evidence capture (still + short clip) | ✅ | ✅ | — |
| Timestamp + geo + officer/device id | ✅ | ✅ | ✅ (validates) |
| SHA-256 of original frame | ✅ | ✅ | ✅ (verifies) |
| Chain-of-custody event on every action | ✅ | ✅ | ✅ (stores) |
| Offline queue + sync | ✅ | ✅ | ✅ (reconciles) |
| Hotlist overlay (plate → status) | ✅ | ✅ | ✅ (returns status) |
| Basic digital zoom + torch + focus | ✅ (where exposed) | ✅ (full) | — |
| **Long-range optical zoom** | ❌ | ✅ | — |
| **Deblurring / low-light / fog** | ❌ (toy-only) | ✅ | — |
| **High-FPS on-device detection** | partial (5–15 FPS) | ✅ (30+ FPS) | — |
| **On-device OCR** | partial (assistive) | ✅ | ✅ (authoritative) |
| **Plate normalization + hotlist match** | — | — | ✅ |
| Burst / continuous tracking mode | ❌ (browser-limited) | ✅ | — |
| Body cam / dashcam / drone ingestion | — | — | ✅ |

PWA "❌" cells are deliberate: we will not implement features the
browser cannot do well, even if asked. We surface them as a "switch to
companion device" affordance instead.

## 3. Evidence chain (the substrate everything plugs into)

Every capture from every source produces an **EvidenceFrame**:

```
EvidenceFrame {
  evidence_id (UUID),
  tenant_id,
  source: 'pwa' | 'native_companion' | 'body_cam' | 'dashcam' |
          'fixed_cam' | 'drone' | 'border' | 'toll',
  source_device_id,
  officer_id (nullable for non-officer sources),
  captured_at (ISO-8601 with TZ),
  geo { lat, lng, accuracy_m, heading?, speed? },
  media_kind: 'photo' | 'video' | 'burst',
  original_s3_key,
  original_sha256 (BYTEA),
  original_bytes,
  enhanced_versions: [{
    version_id, kind: 'deblur'|'denoise'|'crop'|'plate-extract',
    s3_key, sha256, produced_by: 'pwa'|'native'|'server',
    parent_version_id (nullable),
  }],
  hints: {
    detected_plate?: string,
    detector_confidence?: float,
    detector_model?: string,
    detector_run_at?: ts,
  },
  audit_chain: [{ action, actor_user, actor_role, actor_device,
                  details, occurred_at }],
}
```

The hash chain rule is:

1. The **original** frame is immutable. `original_sha256` is computed
   on the device before upload and re-verified server-side. Any
   mismatch triggers an audit alert and rejects the evidence.
2. Enhanced versions are **additive, never destructive**. They carry
   their own `sha256` and link to the parent. The audit chain shows
   who ran which enhancement and with which model version.
3. Every read, export, and view appends to `audit_chain` via
   `evidence_custody` (the table already exists).

Map to existing tables:

- `fine_evidence` stays — extends with `source`, `enhanced_of`,
  `version_kind`, `model_id` columns.
- `evidence_custody` stays — gains `action='enhanced'`, `'redacted'`,
  `'exported_to_court'`.
- New view `v_evidence_chain(evidence_id)` materialises the full
  custody + version tree for a single evidence item.

## 4. Offline state machine (PWA)

> Pass-2 gap analysis found no service worker in `apps/police-pwa`.
> Pass-1 claimed encrypted IndexedDB caching. Step zero is to **verify
> ground truth** by running the PWA and inspecting `navigator.serviceWorker`
> + `indexedDB`. Architecture below assumes we are building this.

States per pending evidence item, persisted in IndexedDB:

```
captured        — frame written to encrypted IDB, hash computed
queued          — in upload queue, awaiting connectivity
uploading       — multipart upload in progress
uploaded        — bytes received by server, awaiting confirm
confirmed       — server returned evidence_id + matched hash
synced          — chain-of-custody event acknowledged
sync_failed     — terminal-ish; retry with backoff
purged          — uploaded + retention window elapsed, local copy wiped
```

Rules:

- Encrypted local storage with AES-GCM. **Device key is derived from
  the officer's session (JWT) + a device-bound secret stored via
  WebAuthn / `CryptoKey` non-extractable.** A stolen device without
  an active session must not yield readable evidence.
- Upload queue is FIFO with exponential backoff (1s → 2 → 4 → 8 → 30
  → 60 → 300s, then idle until next online event).
- Conflict resolution is trivial: original frames are append-only and
  immutable; the server is the source of truth for everything else
  (plate hint, hotlist match). Officer-edited metadata (notes, offence
  code on a draft fine) wins last-write-wins per field, with full
  conflict trace in audit.
- UI surface: an always-visible queue badge (`3 queued · 1 uploading
  · 0 failed`), tappable to a queue inspector. Drafts cannot be
  silently lost.

## 5. Hotlist overlay (PWA + native, same data)

The PWA's "scan → verify → fine" flow already exists. The overlay adds
a low-distraction status strip rendered next to the live preview:

```
NO-AB12345    ◉ insurance OK   ◉ inspection EXPIRED 2024-11    ⚠ WANTED
              conf 0.82 · last seen 2026-04-30 14:02 · district BER
```

- Pulls from `GET /v1/vehicles/by-plate/{plate}` once a candidate
  plate is hinted (by hand entry, on-device detector, or server-side
  ANPR result).
- Tactical palette only: high-contrast text on dimmed video, no
  animations under 100ms cadence, no decorative iconography. Three
  severity tiers: `OK / WARN / CRITICAL` map to neutral / amber / red.
- Critical alerts (wanted, stolen) play a single audible tick and
  vibrate (where the platform exposes the Vibration API). They do
  **not** auto-issue a fine — the officer always confirms.

## 6. On-device perception placement

PWA is **assistive only**. The backend ANPR pipeline is authoritative.
What we do on-device:

- A lightweight detector (target: TF.js or ONNX-Web running a tiny
  YOLO-class model trained on plate boundaries, ~2–4 MB) emits a
  bounding box + crop hint at ~5–10 FPS on mid-range mobile. Used
  *only* to:
  1. Guide the officer's framing (a subtle box overlay).
  2. Pre-crop the still that gets uploaded, reducing bandwidth.
  3. Provide an optimistic plate hint the overlay can render with
     a "(unconfirmed)" badge before the server confirms.
- We do **not** ship multi-country OCR in the PWA. OCR is the
  backend's job and the native companion's job. The PWA is allowed
  to attempt a single-line text read for *the officer's UX* but the
  hint is never sent as authoritative.
- Enhancement (deblur, sharpen, denoise) in the PWA: **deferred**. A
  browser-rendered enhancement produces a derived frame whose
  legal weight is unclear; better to upload the original and let the
  server (or the native companion) produce signed enhanced versions.

Model placement, registry, and rollout:

- Models are versioned artefacts in `model_registry(id, kind, version,
  s3_uri, sha256, min_client_version, deprecated_at)`. PWA fetches the
  currently-blessed `kind='pwa_plate_detector'` model on session start;
  caches in IDB; verifies sha256.
- New `model_runs` row is recorded per inference (offline aggregated,
  uploaded with each evidence frame): `(model_id, ran_at, latency_ms,
  confidence, on_device, evidence_id)`. This becomes the ground truth
  for any later "the detector said the plate was X" claim.

## 7. Native companion — scope outline (not built yet)

We are **not building** the native companion in this iteration; we are
making sure the substrate is shaped so it can land cleanly.

Native companion responsibilities:

- Direct camera2 / AVFoundation control: optical zoom, AE/AF locking,
  RAW capture, HDR bracketing, IR/torch toggle on supported hardware.
- High-FPS on-device perception: YOLO-class detector at 30+ FPS,
  small-CNN OCR, optional super-resolution model for crop upscaling.
- Same evidence-frame contract, same offline queue semantics, same
  chain-of-custody calls — the API surface is identical to the PWA's
  evidence endpoint. The companion is just a different *client* of
  the same backend.
- Distribution model is open: either an official ministry-signed APK
  / iOS enterprise build, or a vendor partnership for rugged
  enforcement hardware. Both consume the same OAuth2 / officer JWT.

## 8. Multi-source architecture (already partially in place)

`anpr_scans.source` enum is the integration point. We will:

- Extend to: `'officer_pwa' | 'officer_native' | 'body_cam' | 'dashcam'
  | 'drone' | 'fixed_cam' | 'toll' | 'border' | 'partner_webhook'`.
- Add `anpr_ingest_clients(id, tenant_id, kind, name, api_key_hash,
  scopes JSONB, created_at, revoked_at)` so each non-officer source
  authenticates with its own API key, scoped to ingest only — they
  cannot issue fines, only feed plates and frames.
- All sources flow into the same `anpr_jobs` async pipeline. Hotlist
  matching, plate normalization, and audit emission are unchanged.

## 9. Data model additions (smallest viable set)

```
-- evidence-frame additions to fine_evidence (or a new evidence_frames
-- table if we want to decouple from fines — TBD)
ALTER TABLE fine_evidence
  ADD COLUMN source TEXT NOT NULL DEFAULT 'officer_pwa',
  ADD COLUMN enhanced_of UUID REFERENCES fine_evidence(id),
  ADD COLUMN version_kind TEXT,   -- 'original' | 'deblur' | 'crop' | ...
  ADD COLUMN model_id UUID,       -- nullable; FK once model_registry lands
  ADD COLUMN device_attestation JSONB;  -- WebAuthn / Play Integrity blob

-- per-model registry
CREATE TABLE model_registry (
  id UUID PRIMARY KEY,
  kind TEXT NOT NULL,             -- 'pwa_plate_detector' | 'server_ocr' | ...
  version TEXT NOT NULL,
  s3_uri TEXT NOT NULL,
  sha256 BYTEA NOT NULL,
  min_client_version TEXT,
  deprecated_at TIMESTAMPTZ,
  UNIQUE (kind, version)
);

-- per-inference audit
CREATE TABLE model_runs (
  id BIGSERIAL PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  model_id UUID NOT NULL REFERENCES model_registry(id),
  evidence_id UUID NOT NULL,
  on_device BOOLEAN NOT NULL,
  ran_at TIMESTAMPTZ NOT NULL,
  latency_ms INTEGER,
  confidence REAL,
  detected_plate TEXT,
  raw_output JSONB
);

-- ingestion clients for non-officer sources
CREATE TABLE anpr_ingest_clients (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,             -- 'body_cam' | 'dashcam' | 'drone' | 'fixed_cam' | 'partner_webhook'
  name TEXT NOT NULL,
  api_key_hash BYTEA NOT NULL,
  scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);
```

All four are tenant-scoped and get the standard RLS `tenant_isolation`
policy. `model_registry` may be global (shared models across tenants);
TBD when we settle on the model-distribution model.

## 10. API surface additions

```
POST   /v1/evidence                  multipart upload + JSON manifest
GET    /v1/evidence/{id}             metadata + signed download URLs
POST   /v1/evidence/{id}/enhance     server-side enhancement job
POST   /v1/evidence/{id}/export      court-export bundle (signed PDF + frames)
GET    /v1/models/pwa_plate_detector return current blessed model artefact
POST   /v1/anpr/ingest               for non-officer sources, API-key auth
```

Existing endpoints stay; the camera flow does **not** invent a parallel
evidence path. `POST /v1/fines` continues to reference evidence by
`evidence_id` rather than embedding raw bytes.

## 11. Security & audit guarantees

- Original frame SHA-256 computed device-side **and** re-computed
  server-side. Mismatch → reject + audit alert.
- Every enhancement records the model id, version, run timestamp, and
  parent frame; the chain is rebuildable.
- Officer JWT carries `device_id`; evidence is rejected if the
  `device_id` claim doesn't match the device attestation header.
- WebAuthn / Play Integrity attestation accepted at upload time;
  failures audited but not always blocking (configurable per tenant).
- A captured frame's `audit_chain` is exposed in the citizen-facing
  appeal flow on request (privacy-redacted), so officers cannot
  silently lose or alter their own evidence.

## 12. Phased implementation

Sequencing aligns with the foundational substrate already in flight,
so we don't fragment effort:

**Phase C0 — Verification & substrate prep** (small)
- Run the PWA, confirm current state of service-worker / IDB.
- Reconcile Pass-1 vs Pass-2 finding.
- Land migration adding `source`, `enhanced_of`, `version_kind`,
  `model_id`, `device_attestation` columns on `fine_evidence`.
- Land `model_registry`, `model_runs`, `anpr_ingest_clients`.

**Phase C1 — Evidence substrate** (medium)
- `POST /v1/evidence` and `GET /v1/evidence/{id}` server endpoints.
- Server-side SHA-256 verify + custody emission on every action.
- Offline queue + encrypted IDB in PWA, with the explicit state
  machine in §4.
- Queue inspector UI.

**Phase C2 — Hotlist overlay** (small-medium)
- Tactical overlay component reading `GET /v1/vehicles/by-plate/{plate}`.
- Three-tier severity, audible tick + haptic on CRITICAL.
- Manual plate entry flow stays primary; detector hint is optional.

**Phase C3 — Assistive on-device detector** (medium)
- Ship a tiny YOLO-class detector via TF.js / ONNX-Web.
- Model registry + signed-download + sha256 verify.
- `model_runs` recorded per inference.
- Pre-crop optimisation for upload bandwidth.

**Phase C4 — Multi-source ingestion** (medium)
- `POST /v1/anpr/ingest` for body-cam / dashcam / drone / fixed-cam /
  partner-webhook clients, gated on `anpr_ingest_clients` API keys.
- Same hotlist + matching pipeline.

**Phase N — Native companion** (out of scope here)
- Separate project. Same evidence contract. Scoped when ministry is
  ready to fund / partner.

## 13. Explicit non-goals (for the PWA)

- Long-distance optical zoom (browser cannot deliver).
- Real-time deblurring or fog / rain compensation (toy-only in a
  browser; downstream legal risk).
- 30+ FPS continuous tracking with on-device ML.
- Burst capture with sustained throughput.
- Body-cam-quality low-light.
- Anything that would mislead an officer about the device's actual
  capability under real roadside conditions.

These belong in the native companion or are simply not promised.

## 14. Open questions (need product / legal input)

1. Are originally-captured frames retained at full fidelity for the
   full retention window, or are they downsampled after N days?
   Affects storage cost and legal admissibility.
2. Are server-side enhancements (deblur, super-resolution) admissible
   as evidence in your target jurisdictions, or only the originals?
   Affects whether C3 needs a "raw-only" capture mode toggle.
3. Officer-issued vs. fixed-camera evidence — do they have the same
   evidentiary weight, or does fixed-camera need a separate
   accreditation chain (calibration certificates, etc.)?
4. WebAuthn / Play Integrity attestation: hard requirement or
   advisory? Affects what we do when a device fails attestation
   mid-shift.

These should be answered before C1 ships, not during.
