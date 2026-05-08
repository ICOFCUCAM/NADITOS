// Package contracts defines the interfaces every external integration
// must satisfy so that NADITOS services depend on stable behavior, not
// on any specific vendor.
//
// Each integration lives in its own sub-package:
//
//   contracts/payments       — payment processors (Stripe, Adyen, sovereign)
//   contracts/anpr           — license-plate recognition engines
//   contracts/insurance      — national insurance bureau / EU green-card
//   contracts/inspection     — roadworthiness station networks
//   contracts/court          — court / judicial workflow
//   contracts/notifications  — SMS / email / push providers
//   contracts/storage        — S3-compatible object storage
//   contracts/identity       — government identity providers (eIDAS, civil registry)
//
// All adapters MUST be deterministic, side-effect-honest, and respect
// Context cancellation. Errors should be wrapped with %w to preserve
// the underlying provider error type.
package contracts

// AdapterInfo lets the platform record which provider implementation is
// active for a given module + tenant in /healthz responses and audit
// events.
type AdapterInfo struct {
	Module   string `json:"module"`   // "payments", "anpr", ...
	Provider string `json:"provider"` // "stripe", "openalpr", "dev-stub"
	Region   string `json:"region,omitempty"`
}
