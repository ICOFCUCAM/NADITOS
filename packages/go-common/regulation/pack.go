// Package regulation defines the data model for country regulation
// packs. A "pack" is a versioned, signed bundle that defines:
//
//   - the catalog of offences (codes, names per locale, base fine,
//     points, duplicate window)
//   - escalation stages (warning → penalty → flag → seize → court)
//   - vehicle categories and inspection intervals
//   - license classes
//   - localized legal templates (citation text, dispute form)
//   - plate regex and currency
//
// Packs are stored in country_packs (DB) and the active one per tenant
// in tenant_country_pack. Services hot-reload them every 30s.
//
// In production, packs ship as signed JSON files reviewed by the
// ministry's legal team. This package validates shape, not policy.
package regulation

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

type Pack struct {
	ID            string         `json:"id"`              // e.g. "NO-2026-01"
	CountryCode   string         `json:"country_code"`
	Version       string         `json:"version"`
	EffectiveFrom time.Time      `json:"effective_from"`
	Locales       []string       `json:"locales"`         // ["en","no"]
	Currency      string         `json:"currency"`
	PlateRegex    string         `json:"plate_regex"`
	Offences      []Offence      `json:"offences"`
	Escalation    []Stage        `json:"escalation"`
	LicenseClasses []LicenseClass `json:"license_classes"`
	VehicleCategories []VehicleCategory `json:"vehicle_categories"`
}

type Offence struct {
	Code           string            `json:"code"`
	Name           map[string]string `json:"name"`           // locale -> string
	Description    map[string]string `json:"description,omitempty"`
	BaseAmount     string            `json:"base_amount"`    // decimal-as-string
	Currency       string            `json:"currency"`
	Points         int               `json:"points"`
	DuplicateWindowMin int            `json:"duplicate_window_min"`
	RuleExpr       string            `json:"rule_expr,omitempty"`
}

type Stage struct {
	Stage      int     `json:"stage"`
	AfterDays  int     `json:"after_days"`
	Multiplier float64 `json:"multiplier"`
	Action     string  `json:"action"` // warning|penalty|flag|seize|court
}

type LicenseClass struct {
	Code        string            `json:"code"`         // A, B, C, ...
	Name        map[string]string `json:"name"`
	MinAge      int               `json:"min_age"`
	MaxWeightKg int               `json:"max_weight_kg,omitempty"`
}

type VehicleCategory struct {
	Code             string `json:"code"`         // car, motorcycle, truck, ...
	InspectionMonths int    `json:"inspection_months"`
}

// Validate checks structural invariants. Returns the first error found.
func (p *Pack) Validate() error {
	if p.ID == "" || p.CountryCode == "" || p.Version == "" {
		return errors.New("pack: id, country_code, version required")
	}
	if p.PlateRegex == "" {
		return errors.New("pack: plate_regex required")
	}
	if _, err := regexp.Compile(p.PlateRegex); err != nil {
		return fmt.Errorf("pack: invalid plate_regex: %w", err)
	}
	if p.Currency == "" {
		return errors.New("pack: currency required")
	}
	if len(p.Offences) == 0 {
		return errors.New("pack: at least one offence required")
	}
	codes := map[string]bool{}
	for _, o := range p.Offences {
		if o.Code == "" {
			return errors.New("pack: offence code missing")
		}
		if codes[o.Code] {
			return fmt.Errorf("pack: duplicate offence code %q", o.Code)
		}
		codes[o.Code] = true
		if o.BaseAmount == "" {
			return fmt.Errorf("pack: offence %q missing base_amount", o.Code)
		}
		if o.DuplicateWindowMin < 0 {
			return fmt.Errorf("pack: offence %q negative dup window", o.Code)
		}
	}
	stages := map[int]bool{}
	for _, s := range p.Escalation {
		if s.Stage < 1 || s.Stage > 5 {
			return fmt.Errorf("pack: escalation stage out of range %d", s.Stage)
		}
		if stages[s.Stage] {
			return fmt.Errorf("pack: duplicate escalation stage %d", s.Stage)
		}
		stages[s.Stage] = true
	}
	return nil
}

// ParseManifest unmarshals a manifest JSON blob into a Pack.
func ParseManifest(b []byte) (*Pack, error) {
	var p Pack
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// OffenceByCode returns the offence record for a given code, or nil.
func (p *Pack) OffenceByCode(code string) *Offence {
	for i := range p.Offences {
		if p.Offences[i].Code == code {
			return &p.Offences[i]
		}
	}
	return nil
}
