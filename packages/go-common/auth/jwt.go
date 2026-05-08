// Package auth handles JWT issuance, parsing, and request authentication.
package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	TenantID    string   `json:"tid"`
	Role        string   `json:"role"`
	Permissions []string `json:"perms"`
	DeviceID    string   `json:"did,omitempty"`
	jwt.RegisteredClaims
}

type Issuer struct {
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

func NewIssuer(secret string, accessTTL, refreshTTL time.Duration) *Issuer {
	return &Issuer{Secret: []byte(secret), AccessTTL: accessTTL, RefreshTTL: refreshTTL}
}

func (i *Issuer) Sign(userID uuid.UUID, c Claims) (string, error) {
	now := time.Now()
	c.RegisteredClaims = jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(i.AccessTTL)),
		ID:        uuid.NewString(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, &c)
	return t.SignedString(i.Secret)
}

func (i *Issuer) Parse(tok string) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(tok, c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "HS256" {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return i.Secret, nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ─── Request context ────────────────────────────────────────────────────────
type ctxKey struct{}

func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}
func ClaimsFrom(ctx context.Context) *Claims {
	c, _ := ctx.Value(ctxKey{}).(*Claims)
	return c
}

// BearerToken extracts the token from the Authorization header.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
