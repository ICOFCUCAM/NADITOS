// Field-side verification of a driver license.
//
// Two flows:
//
//   1. issue-token: the license holder requests a short-lived signed
//      bundle from their citizen app or in-vehicle wallet. The bundle is
//      JSON-encoded, then HMAC-signed with the JWT secret using a key id
//      so secret rotation is possible. It contains license_id, tenant,
//      issued_at, expires_at (≤ 5 min). The QR/NFC just carries the
//      base64 encoding.
//
//   2. verify: the officer scans the QR/NFC and POSTs the bundle. We
//      verify the signature, look up the license, and return current
//      standing. The token can be presented even when the citizen has
//      no connectivity — the officer's PWA validates online.
//
// In production the signing key SHOULD live in a separate KID-rotated
// scheme (we keep it simple here using the JWT secret as the seed).
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type verifyBundle struct {
	V         int    `json:"v"`           // version
	LID       string `json:"lid"`         // license id
	TID       string `json:"tid"`         // tenant id
	Number    string `json:"num"`         // license number
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func (a *API) issueVerifyToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	c := auth.ClaimsFrom(r.Context())

	// Citizens can only request a token for their own license.
	if c.Role == "citizen" {
		var owner *uuid.UUID
		_ = conn.QueryRow(r.Context(),
			`SELECT user_id FROM driver_licenses WHERE id=$1`, id).Scan(&owner)
		if owner == nil || owner.String() != c.Subject {
			httpx.WriteErr(w, httpx.ErrForbidden); return
		}
	}

	var num string
	if err := conn.QueryRow(r.Context(),
		`SELECT license_number FROM driver_licenses WHERE id=$1`, id).Scan(&num); err != nil {
		httpx.WriteErr(w, httpx.ErrNotFound); return
	}

	now := time.Now().UTC()
	b := verifyBundle{
		V: 1, LID: id.String(), TID: c.TenantID, Number: num,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(),
	}
	tok := signBundle(a.cfg.JWTSecret, b)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":      tok,
		"expires_at": time.Unix(b.ExpiresAt, 0).UTC(),
	})
}

func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	type req struct{ Token string `json:"token"` }
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil { httpx.WriteErr(w, err); return }
	b, err := verifyBundleString(a.cfg.JWTSecret, in.Token)
	if err != nil {
		httpx.WriteErr(w, httpx.Err(400, "bad_token", err.Error())); return
	}
	c := auth.ClaimsFrom(r.Context())
	if b.TID != c.TenantID {
		httpx.WriteErr(w, httpx.ErrForbidden); return
	}

	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	id, _ := uuid.Parse(b.LID)
	l, err := scanLicense(conn.QueryRow(r.Context(), licenseSelect+`WHERE id=$1`, id))
	if writeIfNotFound(w, err) { return }
	if err != nil { httpx.WriteErr(w, err); return }

	var st string
	var recent int
	_ = conn.QueryRow(r.Context(),
		`SELECT standing, recent_violations FROM v_driver_standing WHERE license_id=$1`, id).
		Scan(&st, &recent)

	_ = a.audit.Emit(r.Context(), "license.verify", "driver_license", id.String(), nil,
		map[string]string{"checker": c.Subject})
	httpx.WriteJSON(w, http.StatusOK, standing{
		License: l, Standing: st, RecentViolations: recent,
	})
}

// ─── HMAC bundle helpers ────────────────────────────────────────────────────
func signBundle(secret string, b verifyBundle) string {
	body, _ := json.Marshal(b)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := mac.Sum(nil)
	return "n1." + base64.RawURLEncoding.EncodeToString(body) +
		"." + base64.RawURLEncoding.EncodeToString(sig)
}

func verifyBundleString(secret, tok string) (*verifyBundle, error) {
	parts := splitN(tok, '.', 3)
	if len(parts) != 3 || parts[0] != "n1" {
		return nil, errors.New("invalid token format")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return nil, err }
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil { return nil, err }
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, errors.New("signature mismatch")
	}
	var b verifyBundle
	if err := json.Unmarshal(body, &b); err != nil { return nil, err }
	if time.Now().Unix() > b.ExpiresAt {
		return nil, errors.New("token expired")
	}
	return &b, nil
}

func splitN(s string, sep byte, n int) []string {
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(s) && len(out) < n-1; i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
