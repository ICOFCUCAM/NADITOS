# Roadmap

## Phase 1 — MVP foundation (this repository)

- [x] Multi-tenant Postgres schema with RLS
- [x] Auth + JWT + RBAC
- [x] Vehicle Registry with status engine
- [x] Fines engine with evidence-required + duplicate-protection
- [x] Append-only hash-chained audit log
- [x] Admin web app (dashboard + vehicles + fines + audit)
- [x] Police PWA (login + plate scan + lookup + fine issuance)
- [x] Citizen app (vehicles + fines + pay-stub)
- [x] Docker compose for local dev
- [x] K8s manifests + Vercel config
- [x] Driver license module — full lifecycle, demerit engine, QR/NFC verify
- [x] Insurance verification module — provider router + retry queue + health monitor + worker persists policy
- [x] Roadworthiness module — same connector framework + worker persists record
- [x] ANPR gateway — async pipeline with outbox-routed event emission
- [x] Country regulation packs — versioned manifest + hot-reloading loader
- [x] Provider connector framework — retry queue, health monitor, country router
- [x] Domain event bus — InProc + outbox + relay + cross-process consumers
- [x] OpenAPI 3.1 spec for the gateway
- [x] Observability — request id, structured logs, /metrics
- [x] Notifications — consumer drains event_outbox; 7 renderers; citizen inbox
- [x] Vehicle ownership transfer (citizen → citizen) with code + 7-day expiry
- [x] Audit anomaly detection — z-score + cancel-rate detectors → audit_alerts
- [x] ANPR alerts → audit_alerts (stolen / seized / wanted vehicle scans)
- [x] Evidence retention reaper — sealed_at + storage delete + audit custody
- [x] Payment webhook with signature verification + idempotent paid transition
- [x] Race-detector + govulncheck in CI; full-package test coverage

## Phase 2 — production hardening

- [ ] Real ANPR engine (OpenALPR adapter shipped; integrate PlateRecognizer / custom CV)
- [ ] Payment gateway (Stripe + local providers — webhook surface ready)
- [ ] SMS / email providers (Twilio / Vonage / sovereign — Sender contract ready)
- [ ] Court escalation API — Filer contract + dev stub shipped; bind real
- [ ] Insurance / DVLA-equivalent provider connectors
- [ ] Fraud / cloned-plate detection ML
- [ ] Officer anomaly detection ML — z-score + cancel-rate live; richer features still TODO
- [ ] WebAuthn enrollment + SSO (SAML / OIDC) for agencies
- [ ] OpenTelemetry full coverage (request id + structured logs already OTel-shaped)
- [ ] Real NATS JetStream — current shape is in-process bus + outbox relay

## Phase 3 — national integrations

- [ ] Border / customs alerts
- [ ] Tax authority connector
- [ ] National ID / civil registry connector
- [ ] Cross-border enforcement (Schengen / regional alliances)
- [ ] eIDAS digital signatures
- [ ] Speed camera + toll integrations
- [ ] Smart city / ITS integration
- [ ] Predictive risk engine

## Phase 4 — ecosystem

- [ ] Driving school module
- [ ] Vehicle financing verification
- [ ] Public transport regulation
- [ ] Fleet management for ministries
- [ ] Logistics tracking
- [ ] Autonomous vehicle integration
