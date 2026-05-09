package auth_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
)

const testSecret = "test-secret-do-not-use-in-prod-32-bytes-please-thanks!"

// TestSignParse_RoundTrip: a signed token parses back to the same
// claims (tenant, role, permissions, subject). The whole platform's
// authz hangs off this; if the round-trip drops a field, every
// permission check downstream silently breaks.
func TestSignParse_RoundTrip(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	uid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tok, err := iss.Sign(uid, auth.Claims{
		TenantID:    "demo",
		Role:        "officer",
		Permissions: []string{"fines:create", "anpr:scan"},
		DeviceID:    "dev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.TenantID != "demo" {
		t.Errorf("tenant: %s", c.TenantID)
	}
	if c.Role != "officer" {
		t.Errorf("role: %s", c.Role)
	}
	if len(c.Permissions) != 2 || c.Permissions[0] != "fines:create" {
		t.Errorf("perms: %+v", c.Permissions)
	}
	if c.DeviceID != "dev-1" {
		t.Errorf("device: %s", c.DeviceID)
	}
	if c.Subject != uid.String() {
		t.Errorf("subject: %s", c.Subject)
	}
}

// TestParse_DifferentSecret_Fails: a token signed by issuer A must
// fail to verify on issuer B. Otherwise a leaked dev secret could
// validate against a production verifier.
func TestParse_DifferentSecret_Fails(t *testing.T) {
	a := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	b := auth.NewIssuer("a-completely-different-secret-32-bytes!", time.Minute, time.Hour)

	tok, _ := a.Sign(uuid.New(), auth.Claims{TenantID: "t1"})
	if _, err := b.Parse(tok); err == nil {
		t.Fatal("expected parse failure on different-secret issuer")
	}
}

// TestParse_Tampered_Fails: flipping a byte in the payload section
// must invalidate the signature.
func TestParse_Tampered_Fails(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok, _ := iss.Sign(uuid.New(), auth.Claims{TenantID: "t1"})

	// JWT is header.payload.signature — flip a byte in the payload.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token shape: %q", tok)
	}
	bad := []byte(parts[1])
	bad[0] ^= 0x01
	tampered := parts[0] + "." + string(bad) + "." + parts[2]
	if _, err := iss.Parse(tampered); err == nil {
		t.Fatal("tampered token parsed successfully")
	}
}

// TestParse_Expired_Fails: a token whose ExpiresAt is in the past
// must fail to parse, even with the correct secret. Otherwise long-
// lived stolen tokens would be the keys to the kingdom.
func TestParse_Expired_Fails(t *testing.T) {
	// AccessTTL=-1ms → token is already expired at issue time.
	iss := auth.NewIssuer(testSecret, -1*time.Millisecond, time.Hour)
	tok, _ := iss.Sign(uuid.New(), auth.Claims{TenantID: "t1"})
	if _, err := iss.Parse(tok); err == nil {
		t.Fatal("expired token parsed successfully")
	}
}

// TestParse_AlgNone_Rejected: a JWT signed with the "none" algorithm
// (or anything other than HS256) MUST be rejected. This is the
// classic "alg=none" attack the parse callback explicitly guards
// against.
func TestParse_AlgNone_Rejected(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	// A pre-fabricated alg=none token. Header {"alg":"none","typ":"JWT"}
	// in base64url is "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0".
	// Payload {"tid":"x"} is "eyJ0aWQiOiJ4In0". Signature empty.
	noneTok := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ0aWQiOiJ4In0."
	if _, err := iss.Parse(noneTok); err == nil {
		t.Fatal("alg=none token parsed successfully")
	}
}

// TestBearerToken_Header: extracts the token after "Bearer ", returns
// "" otherwise. Routing decisions hang off this.
func TestBearerToken_Header(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"Bearer abc.def.ghi", "abc.def.ghi"},
		{"bearer abc", ""}, // case-sensitive in this impl
		{"Token abc", ""},
		{"", ""},
		{"Bearer ", ""},
	}
	for _, tc := range cases {
		r, _ := http.NewRequest("GET", "/", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		got := auth.BearerToken(r)
		if got != tc.want {
			t.Errorf("header %q: got %q, want %q", tc.header, got, tc.want)
		}
	}
}

// TestClaimsFromContext_RoundTrip: WithClaims + ClaimsFrom is the
// pattern every handler uses to read JWT data. An empty context
// returns nil (not a panic) so handlers can branch cleanly.
func TestClaimsFromContext_RoundTrip(t *testing.T) {
	if c := auth.ClaimsFrom(context.Background()); c != nil {
		t.Fatalf("empty ctx should return nil, got %+v", c)
	}
	want := &auth.Claims{TenantID: "t1", Role: "admin"}
	ctx := auth.WithClaims(context.Background(), want)
	got := auth.ClaimsFrom(ctx)
	if got == nil || got.TenantID != "t1" || got.Role != "admin" {
		t.Fatalf("round-trip: got %+v", got)
	}
}
