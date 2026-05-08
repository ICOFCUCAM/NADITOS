# Security model

## Identity

- **Citizens** — email/phone + password + optional TOTP/WebAuthn
- **Officers** — agency SSO + device binding + biometric on the device
- **Admins** — agency SSO + WebAuthn (mandatory)
- **Service accounts** — short-lived JWTs signed by `auth` service

JWT claims:

```json
{
  "sub": "user-uuid",
  "tid": "tenant-id",
  "role": "officer|admin|citizen|service",
  "perms": ["fines:create","registry:read"],
  "did": "device-id",
  "iat": ..., "exp": ..., "jti": "..."
}
```

Refresh tokens are stored hashed (sha-256) and revocable per device.

## RBAC + ABAC

`packages/go-common/auth` exports a middleware that:

1. verifies the JWT,
2. binds claims into request context,
3. exposes `RequirePermission("fines:create")` and
   `RequireAnyRole("officer","admin")` helpers,
4. sets the Postgres session vars (`app.tenant_id`, `app.user_id`,
   `app.role`) so RLS policies activate.

Permissions are flat strings `<resource>:<action>` and assigned via
`role_permissions`. ABAC rules (e.g. officer-only-in-jurisdiction)
are enforced inside services using context attributes on the JWT.

## Audit

Every state-changing handler MUST call `audit.Emit(ctx, event)` before
returning success. The Go middleware enforces this with a deferred
"missing audit" panic in dev builds — caught and logged in production.

## Evidence integrity

- Photos are uploaded directly to object storage via short-lived
  presigned PUTs.
- The server records the SHA-256 of the uploaded bytes when issuing
  the fine. Tampering breaks the hash and is detected.
- Optional: future support for C2PA-signed cameras for officer devices.

## Network

- mTLS between services in the mesh
- WAF + rate limiting at the edge
- Per-tenant rate budgets for write endpoints
- IP-allow-list for admin endpoints

## Secrets

- No secrets in repo. `.env.example` is sample only.
- Production: read from cloud secret manager (AWS Secrets Manager,
  GCP Secret Manager, HashiCorp Vault, sovereign equivalents).

## GDPR / data protection

- All PII columns marked `pii=true` in metadata
- Per-tenant data residency (separate cluster or schema)
- Subject Access Request endpoint (`/me/export`)
- Right-to-erasure honored except for legally retained records
  (audit log, paid fines), which are pseudonymized on erasure.

## Incident response

- Audit chain verifier runs hourly, alerts on mismatch
- Anomaly detector flags officers with outlier fine patterns
- All admin actions on the audit table are themselves audited
