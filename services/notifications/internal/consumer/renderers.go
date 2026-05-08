package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/events"
)

// recipient is a resolved (channel, address) pair plus the contact
// person's preferred locale for templating.
type recipient struct {
	Channel string // sms|email|push
	Address string // phone or email
	Name    string
	Locale  string
}

// renderer turns an event into a notification.
type renderer struct {
	template string
	resolve  func(ctx context.Context, tx pgx.Tx, env events.Envelope) (*recipient, error)
	render   func(env events.Envelope, r *recipient) (subject, body string)
}

// renderers is the registry mapping event types to their renderer.
// New event subscribers are added here.
var renderers = map[string]renderer{
	events.TypeFineIssued:       fineIssuedRenderer,
	events.TypeFinePaid:         finePaidRenderer,
	events.TypeLicenseSuspended: licenseSuspendedRenderer,
	events.TypeLicenseReinstated: licenseReinstatedRenderer,
}

// ─── Resolvers ──────────────────────────────────────────────────────────────

// resolveByVehicle: finds the vehicle's owner and returns their email
// (or phone). Returns (nil, nil) when no contact info is on file —
// notifications service records that as 'suppressed' for visibility.
func resolveByVehicle(ctx context.Context, tx pgx.Tx, tenant, vehicleID string) (*recipient, error) {
	if vehicleID == "" {
		return nil, nil
	}
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(u.email::text, o.email::text, ''),
		        COALESCE(u.phone, o.phone, ''),
		        COALESCE(u.full_name, o.full_name, '')
		   FROM vehicles v
		   LEFT JOIN owners o ON o.id = v.owner_id
		   LEFT JOIN users  u ON u.id = o.user_id
		  WHERE v.id = $1 AND v.tenant_id = $2`,
		vehicleID, tenant)
	var email, phone, name string
	if err := row.Scan(&email, &phone, &name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	switch {
	case email != "":
		return &recipient{Channel: "email", Address: email, Name: name, Locale: "en"}, nil
	case phone != "":
		return &recipient{Channel: "sms", Address: phone, Name: name, Locale: "en"}, nil
	}
	return nil, nil
}

// resolveByLicense: finds the citizen behind a driver license.
func resolveByLicense(ctx context.Context, tx pgx.Tx, tenant, licenseID string) (*recipient, error) {
	if licenseID == "" {
		return nil, nil
	}
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(u.email::text, ''), COALESCE(u.phone, ''),
		        COALESCE(u.full_name, l.full_name, '')
		   FROM driver_licenses l
		   LEFT JOIN users u ON u.id = l.user_id
		  WHERE l.id = $1::uuid AND l.tenant_id = $2`,
		licenseID, tenant)
	var email, phone, name string
	if err := row.Scan(&email, &phone, &name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	switch {
	case email != "":
		return &recipient{Channel: "email", Address: email, Name: name, Locale: "en"}, nil
	case phone != "":
		return &recipient{Channel: "sms", Address: phone, Name: name, Locale: "en"}, nil
	}
	return nil, nil
}

// ─── Renderers per event type ───────────────────────────────────────────────

var fineIssuedRenderer = renderer{
	template: "fine.issued.v1",
	resolve: func(ctx context.Context, tx pgx.Tx, env events.Envelope) (*recipient, error) {
		var p events.FineIssuedPayload
		if err := decodeData(env.Data, &p); err != nil {
			return nil, err
		}
		return resolveByVehicle(ctx, tx, env.TenantID, p.VehicleID)
	},
	render: func(env events.Envelope, r *recipient) (string, string) {
		var p events.FineIssuedPayload
		_ = decodeData(env.Data, &p)
		subject := fmt.Sprintf("Traffic fine issued for %s", p.Plate)
		body := fmt.Sprintf(
			"Hello %s,\n\n"+
				"A fine of %s %s has been issued for vehicle %s\n"+
				"Offence code: %s\n\n"+
				"You can pay or dispute it via the citizen portal.\n\n"+
				"NADITOS",
			r.Name, p.Amount, p.Currency, p.Plate, p.OffenceCode)
		return subject, body
	},
}

var finePaidRenderer = renderer{
	template: "fine.paid.v1",
	resolve: func(ctx context.Context, tx pgx.Tx, env events.Envelope) (*recipient, error) {
		var p events.FinePaidPayload
		if err := decodeData(env.Data, &p); err != nil {
			return nil, err
		}
		// Find the fine to get the vehicle.
		var vehicleID string
		_ = tx.QueryRow(ctx,
			`SELECT COALESCE(vehicle_id::text,'') FROM fines
			  WHERE id=$1 AND tenant_id=$2`, p.FineID, env.TenantID).Scan(&vehicleID)
		return resolveByVehicle(ctx, tx, env.TenantID, vehicleID)
	},
	render: func(env events.Envelope, r *recipient) (string, string) {
		var p events.FinePaidPayload
		_ = decodeData(env.Data, &p)
		subject := "Payment received"
		body := fmt.Sprintf(
			"Hello %s,\n\nYour payment of %s %s for fine %s has been recorded.\n\nNADITOS",
			r.Name, p.Amount, p.Currency, p.FineID[:8])
		return subject, body
	},
}

var licenseSuspendedRenderer = renderer{
	template: "license.suspended.v1",
	resolve: func(ctx context.Context, tx pgx.Tx, env events.Envelope) (*recipient, error) {
		var p events.LicenseSuspendedPayload
		if err := decodeData(env.Data, &p); err != nil {
			return nil, err
		}
		return resolveByLicense(ctx, tx, env.TenantID, p.LicenseID)
	},
	render: func(env events.Envelope, r *recipient) (string, string) {
		var p events.LicenseSuspendedPayload
		_ = decodeData(env.Data, &p)
		subject := "License suspended"
		body := fmt.Sprintf(
			"Hello %s,\n\n"+
				"Your driver license has been suspended.\n"+
				"Reason: %s\n"+
				"Effective: %s\n"+
				"Until: %s\n\n"+
				"NADITOS",
			r.Name, p.Reason, p.StartsAt, p.EndsAt)
		return subject, body
	},
}

// licenseReinstatedRenderer closes the demerit loop: when the lift
// handler clears a suspension we tell the citizen they can drive again.
var licenseReinstatedRenderer = renderer{
	template: "license.reinstated.v1",
	resolve: func(ctx context.Context, tx pgx.Tx, env events.Envelope) (*recipient, error) {
		var p events.LicenseReinstatedPayload
		if err := decodeData(env.Data, &p); err != nil {
			return nil, err
		}
		return resolveByLicense(ctx, tx, env.TenantID, p.LicenseID)
	},
	render: func(env events.Envelope, r *recipient) (string, string) {
		subject := "Driver license reinstated"
		body := fmt.Sprintf(
			"Hello %s,\n\n"+
				"Good news — your driver license has been reinstated and is "+
				"valid for use immediately.\n\n"+
				"NADITOS",
			r.Name)
		return subject, body
	},
}

// decodeData copes with both typed-payload and map[string]any forms,
// because in-process publishes deliver typed and JSON publishes deliver
// map[string]any.
func decodeData(raw any, out any) error {
	if raw == nil {
		return errors.New("nil event data")
	}
	// Round-trip through JSON — handles both typed and map forms.
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
