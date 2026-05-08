package events

// Stable event type strings. New events MUST be added here, not invented
// by services inline. Bump Version on breaking changes; consumers can
// support multiple versions during migration.
const (
	TypeFineIssued    = "fine.issued"     // v1
	TypeFinePaid      = "fine.paid"       // v1
	TypeFineCancelled = "fine.cancelled"  // v1
	TypeFineDisputed  = "fine.disputed"   // v1
	TypeFineEscalated = "fine.escalated"  // v1

	TypeVehicleCreated   = "vehicle.created"   // v1
	TypeVehicleUpdated   = "vehicle.updated"   // v1
	TypeVehicleFlagged   = "vehicle.flagged"   // v1
	TypeVehicleStatusBlack = "vehicle.status.black" // v1

	TypeAnprScan        = "anpr.scan"          // v1
	TypeAnprMatched     = "anpr.matched"       // v1 — scan matched a known vehicle
	TypeAnprAlert       = "anpr.alert"         // v1 — scan matched stolen/seized/wanted

	TypeLicenseIssued     = "license.issued"      // v1
	TypeLicenseUpdated    = "license.updated"     // v1
	TypeLicenseSuspended  = "license.suspended"   // v1
	TypeLicenseReinstated = "license.reinstated"  // v1
	TypeLicenseDemerit    = "license.demerit"     // v1

	TypeProviderHealth   = "provider.health"      // v1
	TypeEvidenceCaptured = "evidence.captured"    // v1
	TypeEvidenceVerified = "evidence.verified"    // v1

	TypeUserLoggedIn    = "user.logged_in"     // v1
	TypeUserLoggedOut   = "user.logged_out"    // v1
)

// Payload structs match the Data field of each event by Type/Version.

type FineIssuedPayload struct {
	FineID       string  `json:"fine_id"`
	Plate        string  `json:"plate"`
	VehicleID    string  `json:"vehicle_id,omitempty"`
	OffenceCode  string  `json:"offence_code"`
	Amount       string  `json:"amount"`
	Currency     string  `json:"currency"`
	IssuedBy     string  `json:"issued_by"`
	DeviceID     string  `json:"device_id,omitempty"`
	GeoLat       float64 `json:"geo_lat"`
	GeoLng       float64 `json:"geo_lng"`
	EvidenceN    int     `json:"evidence_count"`
}

type FinePaidPayload struct {
	FineID      string `json:"fine_id"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	ProviderRef string `json:"provider_ref"`
}

type FineCancelledPayload struct {
	FineID string `json:"fine_id"`
	Reason string `json:"reason"`
}

type FineDisputedPayload struct {
	FineID  string `json:"fine_id"`
	FiledBy string `json:"filed_by"`
	Reason  string `json:"reason"`
}

type FineEscalatedPayload struct {
	FineID    string `json:"fine_id"`
	FromStage int    `json:"from_stage"`
	ToStage   int    `json:"to_stage"`
	Action    string `json:"action"`     // warning|penalty|flag|seize|court
	NewStatus string `json:"new_status"` // resulting fine_status, if changed
}

type VehicleCreatedPayload struct {
	VehicleID string `json:"vehicle_id"`
	Plate     string `json:"plate"`
	OwnerID   string `json:"owner_id,omitempty"`
}

type VehicleFlaggedPayload struct {
	VehicleID string `json:"vehicle_id"`
	Plate     string `json:"plate"`
	IsStolen  bool   `json:"is_stolen"`
	IsSeized  bool   `json:"is_seized"`
	IsWanted  bool   `json:"is_wanted"`
}

type AnprScanPayload struct {
	ScanID           string  `json:"scan_id"`
	Plate            string  `json:"plate"`
	Confidence       float32 `json:"confidence"`
	Source           string  `json:"source"`
	GeoLat           float64 `json:"geo_lat"`
	GeoLng           float64 `json:"geo_lng"`
	MatchedVehicleID string  `json:"matched_vehicle_id,omitempty"`
}

type AnprAlertPayload struct {
	ScanID    string `json:"scan_id"`
	Plate     string `json:"plate"`
	VehicleID string `json:"vehicle_id"`
	IsStolen  bool   `json:"is_stolen"`
	IsSeized  bool   `json:"is_seized"`
	IsWanted  bool   `json:"is_wanted"`
}

type LicenseIssuedPayload struct {
	LicenseID     string   `json:"license_id"`
	LicenseNumber string   `json:"license_number"`
	UserID        string   `json:"user_id,omitempty"`
	Classes       []string `json:"classes"`
}

type LicenseSuspendedPayload struct {
	LicenseID    string `json:"license_id"`
	Reason       string `json:"reason"`
	TriggerKind  string `json:"trigger_kind"`
	StartsAt     string `json:"starts_at"`
	EndsAt       string `json:"ends_at,omitempty"`
}

type LicenseReinstatedPayload struct {
	LicenseID     string `json:"license_id"`
	SuspensionID  string `json:"suspension_id"`
	LiftedReason  string `json:"lifted_reason,omitempty"`
}

type LicenseDemeritPayload struct {
	LicenseID    string `json:"license_id"`
	Delta        int    `json:"delta"`
	Reason       string `json:"reason"`
	Source       string `json:"source"`
	SourceID     string `json:"source_id,omitempty"`
	NewTotal     int    `json:"new_total"`
}

type ProviderHealthPayload struct {
	Module    string `json:"module"`
	Provider  string `json:"provider"`
	State     string `json:"state"`     // ok|degraded|down
	Streak    int    `json:"streak"`
}

type EvidenceCapturedPayload struct {
	EvidenceID string `json:"evidence_id"`
	FineID     string `json:"fine_id"`
	S3Key      string `json:"s3_key"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
}
