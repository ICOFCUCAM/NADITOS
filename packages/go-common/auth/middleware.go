package auth

import (
	"net/http"
	"slices"

	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// Middleware verifies the JWT and binds Claims into the request context.
// It also sets X-Tenant-Id from claims if absent.
func (i *Issuer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := BearerToken(r)
		if tok == "" {
			httpx.WriteErr(w, httpx.ErrUnauthorized)
			return
		}
		c, err := i.Parse(tok)
		if err != nil {
			httpx.WriteErr(w, httpx.ErrUnauthorized)
			return
		}
		// tenant header must match claim (defense in depth)
		if h := r.Header.Get("X-Tenant-Id"); h != "" && h != c.TenantID {
			httpx.WriteErr(w, httpx.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), c)))
	})
}

// RequirePermission denies the request if the caller lacks the permission.
func RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := ClaimsFrom(r.Context())
			if c == nil {
				httpx.WriteErr(w, httpx.ErrUnauthorized)
				return
			}
			if !slices.Contains(c.Permissions, perm) {
				httpx.WriteErr(w, httpx.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAnyRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := ClaimsFrom(r.Context())
			if c == nil {
				httpx.WriteErr(w, httpx.ErrUnauthorized)
				return
			}
			if !slices.Contains(roles, c.Role) {
				httpx.WriteErr(w, httpx.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func HashPasswordOK() {} // see password.go
