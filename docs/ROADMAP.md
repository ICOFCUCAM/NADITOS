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
- [x] Insurance verification module — provider router + retry queue + health monitor
- [x] Roadworthiness module — same connector framework
- [x] ANPR gateway — async pipeline with normalization, dedup, event emission
- [x] Country regulation packs — versioned manifest + hot-reloading loader
- [x] Provider connector framework — retry queue, health monitor, country router
- [x] Domain event bus — InProc + NATS-shaped transport + DB outbox relay
- [x] OpenAPI 3.1 spec for the gateway
- [x] Observability — request id, structured logs, metrics endpoint
- [ ] Notifications (scaffold ready)

## Phase 2 — production hardening

- [ ] Real ANPR engine (OpenALPR / PlateRecognizer / custom CV)
- [ ] Payment gateway (Stripe + local providers)
- [ ] SMS / email providers (Twilio / Vonage / sovereign)
- [ ] Court escalation API
- [ ] Insurance / DVLA-equivalent provider connectors
- [ ] Fraud / cloned-plate detection ML
- [ ] Officer anomaly detection ML
- [ ] WebAuthn enrollment + SSO (SAML / OIDC) for agencies
- [ ] OpenTelemetry full coverage

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
