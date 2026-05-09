package regulation_test

import (
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/regulation"
)

const validPackJSON = `{
  "id": "NO-2026-01",
  "country_code": "NO",
  "version": "1.0",
  "currency": "NOK",
  "plate_regex": "^[A-Z]{2}[0-9]{5}$",
  "offences": [
    {"code":"SPEED_30","name":{"en":"Speeding 30km/h over"},
     "base_amount":"6000.00","currency":"NOK","points":6,"duplicate_window_min":15}
  ],
  "escalation": [
    {"stage":1,"after_days":7,"multiplier":1.0,"action":"warning"},
    {"stage":2,"after_days":14,"multiplier":1.5,"action":"penalty"}
  ]
}`

// TestParseManifest_Valid: a well-shaped pack parses and validates.
func TestParseManifest_Valid(t *testing.T) {
	p, err := regulation.ParseManifest([]byte(validPackJSON))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "NO-2026-01" {
		t.Fatalf("id: %s", p.ID)
	}
	if len(p.Offences) != 1 {
		t.Fatalf("offences: %d", len(p.Offences))
	}
}

// TestValidate_RequiresCoreFields: id, country_code, version,
// plate_regex, currency, and at least one offence are mandatory.
// Missing any → error.
func TestValidate_RequiresCoreFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(string) string
		wantSub string
	}{
		{"no id", func(s string) string { return strings.Replace(s, `"NO-2026-01"`, `""`, 1) }, "id"},
		{"no country", func(s string) string { return strings.Replace(s, `"NO",`, `"",`, 1) }, "country_code"},
		{"no plate_regex", func(s string) string {
			return strings.Replace(s, `"^[A-Z]{2}[0-9]{5}$"`, `""`, 1)
		}, "plate_regex"},
		{"bad plate_regex", func(s string) string {
			return strings.Replace(s, `"^[A-Z]{2}[0-9]{5}$"`, `"[unbalanced"`, 1)
		}, "plate_regex"},
		{"no currency", func(s string) string {
			return strings.Replace(s, `"NOK",`, `"",`, 1)
		}, "currency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := regulation.ParseManifest([]byte(tc.mutate(validPackJSON)))
			if err == nil {
				t.Fatalf("want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err: %v (want substring %q)", err, tc.wantSub)
			}
		})
	}
}

// TestValidate_DuplicateOffenceCode: two offences with the same code
// in one pack are a structural error — the catalog must be unique.
func TestValidate_DuplicateOffenceCode(t *testing.T) {
	dup := strings.Replace(validPackJSON,
		`"offences": [`,
		`"offences": [
		   {"code":"SPEED_30","name":{"en":"Dup"},"base_amount":"100","currency":"NOK","points":1,"duplicate_window_min":1},`,
		1)
	_, err := regulation.ParseManifest([]byte(dup))
	if err == nil || !strings.Contains(err.Error(), "duplicate offence code") {
		t.Fatalf("want duplicate-code error, got %v", err)
	}
}

// TestValidate_OffenceMissingAmount: any offence without base_amount
// is rejected — otherwise an officer's fine would be priced at 0.
func TestValidate_OffenceMissingAmount(t *testing.T) {
	bad := strings.Replace(validPackJSON,
		`"base_amount":"6000.00"`, `"base_amount":""`, 1)
	_, err := regulation.ParseManifest([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), "base_amount") {
		t.Fatalf("want base_amount error, got %v", err)
	}
}

// TestValidate_StageRange: escalation stages must be 1..5.
func TestValidate_StageRange(t *testing.T) {
	bad := strings.Replace(validPackJSON,
		`{"stage":1,"after_days":7,`,
		`{"stage":99,"after_days":7,`, 1)
	_, err := regulation.ParseManifest([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), "stage out of range") {
		t.Fatalf("want stage-range error, got %v", err)
	}
}

// TestOffenceByCode: returns the matching offence, or nil for unknown.
func TestOffenceByCode(t *testing.T) {
	p, _ := regulation.ParseManifest([]byte(validPackJSON))
	if o := p.OffenceByCode("SPEED_30"); o == nil || o.Points != 6 {
		t.Fatalf("by code: %+v", o)
	}
	if o := p.OffenceByCode("MADE_UP"); o != nil {
		t.Fatalf("unknown code should return nil, got %+v", o)
	}
}
