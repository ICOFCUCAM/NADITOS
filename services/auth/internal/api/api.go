// Package api wires the HTTP surface of the auth service.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/observability"
)

type API struct {
	cfg    config.Service
	log    *slog.Logger
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	audit  *audit.Client
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool, auditCl *audit.Client) http.Handler {
	a := &API{
		cfg:    cfg,
		log:    log,
		pool:   pool,
		issuer: auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL),
		audit:  auditCl,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/login",   a.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", a.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout",  a.handleLogout)
	mux.HandleFunc("GET /v1/auth/me",      a.handleMe)
	mux.HandleFunc("POST /v1/admin/users",  a.handleAdminCreateUser)
	return mux
}

// ─── DTOs ───────────────────────────────────────────────────────────────────
type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Tenant   string `json:"tenant,omitempty"`
}
type tokenResp struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         meResp    `json:"user"`
}
type meResp struct {
	ID          string   `json:"id"`
	Tenant      string   `json:"tenant"`
	Email       string   `json:"email"`
	FullName    string   `json:"full_name"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	// TenantConfig is the jurisdiction-level configuration the
	// frontend needs to render forms correctly: plate format, currency
	// for fine amounts, and so on. It's pulled fresh from the tenants
	// row at login + /me time so a ministry change to e.g. plate_regex
	// flows to clients on the next session refresh.
	TenantConfig tenantConfig `json:"tenant_config"`
}

type tenantConfig struct {
	Name        string `json:"name"`
	CountryCode string `json:"country_code"`
	Locale      string `json:"locale"`
	Currency    string `json:"currency"`
	PlateRegex  string `json:"plate_regex"`
}

// ─── Handlers ───────────────────────────────────────────────────────────────
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rid, _, _ := observability.IDs(ctx)

	var req loginReq
	if err := httpx.ReadJSON(r, &req); err != nil {
		a.log.Warn("login: bad request body",
			slog.String("rid", rid), slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	tenantSrc := "body"
	tenant := req.Tenant
	if tenant == "" {
		tenant = r.Header.Get("X-Tenant-Id")
		tenantSrc = "header"
	}
	if tenant == "" {
		tenant = a.cfg.DefaultTenant
		tenantSrc = "default"
	}
	lg := a.log.With(
		slog.String("rid", rid),
		slog.String("step", "login"),
		slog.String("tenant", tenant),
		slog.String("email", req.Email),
	)
	lg.Info("login: start", slog.String("tenant_src", tenantSrc))

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		lg.Error("login: begin tx failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	// The auth service is the single component that operates across all
	// tenants (login is the moment we *establish* a tenant for the request).
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		lg.Error("login: SET LOCAL row_security=off failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}

	var (
		userID       uuid.UUID
		passwordHash string
		fullName     string
		isActive     bool
	)
	err = tx.QueryRow(ctx,
		`SELECT id, password_hash, full_name, is_active
		   FROM users
		  WHERE tenant_id=$1 AND email=$2`,
		tenant, req.Email).Scan(&userID, &passwordHash, &fullName, &isActive)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		lg.Info("login: user not found")
		a.emitLoginFailure(ctx, tenant, "", req.Email, "user_not_found", r)
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	case err != nil:
		lg.Error("login: user lookup query failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	case !isActive:
		lg.Info("login: user inactive", slog.String("user_id", userID.String()))
		a.emitLoginFailure(ctx, tenant, userID.String(), req.Email, "user_inactive", r)
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}
	lg = lg.With(slog.String("user_id", userID.String()))
	lg.Info("login: user found, verifying password")

	if err := auth.CheckPassword(passwordHash, req.Password); err != nil {
		lg.Info("login: password mismatch", slog.String("err", err.Error()))
		a.emitLoginFailure(ctx, tenant, userID.String(), req.Email, "bad_password", r)
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}

	role, perms, err := loadRoleAndPerms(withLogger(ctx, lg), tx, tenant, userID)
	if err != nil {
		lg.Error("login: load role/perms failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	lg.Info("login: roles loaded",
		slog.String("role", role), slog.Int("perm_count", len(perms)))

	access, err := a.issuer.Sign(userID, auth.Claims{
		TenantID:    tenant,
		Role:        role,
		Permissions: perms,
	})
	if err != nil {
		lg.Error("login: JWT sign failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}

	refresh, refreshHash := newRefreshToken()
	exp := time.Now().Add(a.cfg.RefreshTTL)
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (tenant_id, user_id, token_hash, expires_at)
		 VALUES ($1,$2,$3,$4)`,
		tenant, userID, refreshHash, exp); err != nil {
		lg.Error("login: insert refresh_token failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		lg.Error("login: commit failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	lg.Info("login: success")
	a.emitLoginSuccess(ctx, tenant, userID.String(), req.Email, role, r)

	tcfg, err := a.loadTenantConfig(ctx, tenant)
	if err != nil {
		lg.Warn("login: tenant config load failed (continuing)", slog.String("err", err.Error()))
	}

	httpx.WriteJSON(w, http.StatusOK, tokenResp{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(a.cfg.AccessTTL),
		User: meResp{
			ID: userID.String(), Tenant: tenant, Email: req.Email,
			FullName: fullName, Role: role, Permissions: perms,
			TenantConfig: tcfg,
		},
	})
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rid, _, _ := observability.IDs(ctx)
	lg := a.log.With(slog.String("rid", rid), slog.String("step", "refresh"))

	var req struct{ RefreshToken string `json:"refresh_token"` }
	if err := httpx.ReadJSON(r, &req); err != nil {
		lg.Warn("refresh: bad request body", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	hash := hashToken(req.RefreshToken)

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		lg.Error("refresh: begin tx failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		lg.Error("refresh: SET LOCAL failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}

	var (
		id        uuid.UUID
		userID    uuid.UUID
		tenant    string
		expiresAt time.Time
		revokedAt *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, expires_at, revoked_at
		   FROM refresh_tokens WHERE token_hash=$1`, hash).
		Scan(&id, &userID, &tenant, &expiresAt, &revokedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		lg.Info("refresh: token not found")
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	case err != nil:
		lg.Error("refresh: lookup query failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	case revokedAt != nil:
		lg.Info("refresh: token revoked", slog.String("user_id", userID.String()))
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	case time.Now().After(expiresAt):
		lg.Info("refresh: token expired", slog.String("user_id", userID.String()))
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}

	role, perms, err := loadRoleAndPerms(withLogger(ctx, lg), tx, tenant, userID)
	if err != nil {
		lg.Error("refresh: load role/perms failed",
			slog.String("user_id", userID.String()), slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		lg.Error("refresh: commit failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	access, err := a.issuer.Sign(userID, auth.Claims{
		TenantID: tenant, Role: role, Permissions: perms,
	})
	if err != nil {
		lg.Error("refresh: JWT sign failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": access,
		"expires_at":   time.Now().Add(a.cfg.AccessTTL),
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct{ RefreshToken string `json:"refresh_token"` }
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	hash := hashToken(req.RefreshToken)
	_, _ = a.pool.Exec(r.Context(),
		`UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1`, hash)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	tok := auth.BearerToken(r)
	if tok == "" {
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}
	c, err := a.issuer.Parse(tok)
	if err != nil {
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}
	tcfg, err := a.loadTenantConfig(r.Context(), c.TenantID)
	if err != nil {
		// Don't 500 the page; an empty tenant config is degraded but
		// usable. Log and continue.
		a.log.Warn("me: tenant config load failed",
			slog.String("tenant", c.TenantID), slog.String("err", err.Error()))
	}
	httpx.WriteJSON(w, http.StatusOK, meResp{
		ID: c.Subject, Tenant: c.TenantID,
		Role: c.Role, Permissions: c.Permissions,
		TenantConfig: tcfg,
	})
}

// loadTenantConfig reads jurisdiction-level config (plate_regex,
// currency, country code, …) from the tenants row. It runs with
// row_security off so the auth service — which operates across
// tenants — can read the row regardless of the caller's app session
// vars.
func (a *API) loadTenantConfig(ctx context.Context, tenant string) (tenantConfig, error) {
	var tc tenantConfig
	conn, err := a.pool.Acquire(ctx)
	if err != nil {
		return tc, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return tc, err
	}
	err = conn.QueryRow(ctx,
		`SELECT name, country_code, default_locale, currency, plate_regex
		   FROM tenants WHERE id = $1`, tenant).
		Scan(&tc.Name, &tc.CountryCode, &tc.Locale, &tc.Currency, &tc.PlateRegex)
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, nil
	}
	return tc, err
}

// handleAdminCreateUser is the user-creation endpoint used by seed scripts
// and by admin UIs. It accepts either of two forms of authorization,
// checked in order:
//
//  1. An ADMIN_BOOTSTRAP_KEY shared secret presented in the
//     X-Admin-Bootstrap-Key request header. Intended for first-run
//     seeding (no admin user exists yet to issue an admin JWT).
//  2. A valid bearer JWT whose role == "admin". Intended for ongoing
//     user-management calls from an authenticated admin UI.
//
// If ADMIN_BOOTSTRAP_KEY is unset in env the bootstrap path is closed
// (the only way in is then an admin JWT). Either form is sufficient on
// its own — operators can rotate the bootstrap key and rely on the JWT
// path once the first admin exists.
func (a *API) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rid, _, _ := observability.IDs(ctx)

	if !a.adminAuthorized(r) {
		a.log.Warn("admin_create_user: unauthorized",
			slog.String("rid", rid),
			slog.Bool("had_bootstrap_header", r.Header.Get("X-Admin-Bootstrap-Key") != ""),
			slog.Bool("had_bearer", auth.BearerToken(r) != ""),
		)
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}

	type req struct {
		Email    string   `json:"email"`
		Password string   `json:"password"`
		FullName string   `json:"full_name"`
		Roles    []string `json:"roles"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		a.log.Warn("admin_create_user: bad request body",
			slog.String("rid", rid), slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	tenant := r.Header.Get("X-Tenant-Id")
	if tenant == "" {
		tenant = a.cfg.DefaultTenant
	}
	lg := a.log.With(
		slog.String("rid", rid),
		slog.String("step", "admin_create_user"),
		slog.String("tenant", tenant),
		slog.String("email", in.Email),
	)
	lg.Info("admin_create_user: start", slog.Any("roles", in.Roles))

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		lg.Error("admin_create_user: hash password failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		lg.Error("admin_create_user: begin tx failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		lg.Error("admin_create_user: SET LOCAL failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, password_hash, full_name)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (tenant_id, email) DO UPDATE SET password_hash=EXCLUDED.password_hash
		 RETURNING id`,
		tenant, in.Email, hash, in.FullName).Scan(&id)
	if err != nil {
		lg.Error("admin_create_user: upsert user failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	for _, role := range in.Roles {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_roles (tenant_id, user_id, role_code)
			 VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			tenant, id, role); err != nil {
			lg.Error("admin_create_user: insert user_role failed",
				slog.String("role", role), slog.String("err", err.Error()))
			httpx.WriteErr(w, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		lg.Error("admin_create_user: commit failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	}
	lg.Info("admin_create_user: success", slog.String("user_id", id.String()))
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// adminAuthorized returns true iff the request carries either:
//   - the ADMIN_BOOTSTRAP_KEY env value in the X-Admin-Bootstrap-Key
//     header (constant-time compared, so timing attacks don't leak it),
//     or
//   - a bearer JWT whose role claim is "admin" and which verifies under
//     the service's signing secret.
//
// Returns false if neither is present or both are present-but-invalid.
// An empty/missing ADMIN_BOOTSTRAP_KEY closes the shared-secret path.
func (a *API) adminAuthorized(r *http.Request) bool {
	if k := os.Getenv("ADMIN_BOOTSTRAP_KEY"); k != "" {
		got := r.Header.Get("X-Admin-Bootstrap-Key")
		// subtle.ConstantTimeCompare needs equal-length inputs; do a fast
		// length check first (length isn't sensitive, the bytes are).
		if len(got) == len(k) && subtleEq(got, k) {
			return true
		}
	}
	if tok := auth.BearerToken(r); tok != "" {
		if c, err := a.issuer.Parse(tok); err == nil && c != nil && c.Role == "admin" {
			return true
		}
	}
	return false
}

// subtleEq is a tiny constant-time equality check. We use strings rather
// than []byte to avoid a copy at the call site; both inputs are already
// strings.
func subtleEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// Querier is the small subset of pgx we need; both *pgx.Conn and pgx.Tx satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadRoleAndPerms returns the user's primary role and flattened permissions.
// The "primary" role is admin > court > customs > officer > citizen, in that order.
//
// The function does its own per-stage logging so that "login: load role/perms
// failed" in the caller is always paired with a precise reason (which query,
// which scan) here. lg may be nil — useful for tests and the seed CLI.
func loadRoleAndPerms(ctx context.Context, conn Querier, tenant string, userID uuid.UUID) (string, []string, error) {
	lg := loggerFromCtx(ctx).With(
		slog.String("step", "load_role_perms"),
		slog.String("tenant", tenant),
		slog.String("user_id", userID.String()),
	)

	rows, err := conn.Query(ctx,
		`SELECT role_code FROM user_roles WHERE tenant_id=$1 AND user_id=$2`,
		tenant, userID)
	if err != nil {
		lg.Error("user_roles query failed", slog.String("err", err.Error()))
		return "", nil, err
	}
	roles := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			lg.Error("user_roles scan failed", slog.String("err", err.Error()))
			return "", nil, err
		}
		roles = append(roles, r)
	}
	rows.Close()
	if len(roles) == 0 {
		lg.Info("no roles assigned, defaulting to citizen")
		return "citizen", nil, nil
	}
	primary := pickPrimary(roles)

	prows, err := conn.Query(ctx,
		`SELECT DISTINCT permission FROM role_permissions
		   WHERE tenant_id=$1 AND role_code = ANY($2)`,
		tenant, roles)
	if err != nil {
		lg.Error("role_permissions query failed",
			slog.Any("roles", roles), slog.String("err", err.Error()))
		return "", nil, err
	}
	defer prows.Close()
	perms := []string{}
	for prows.Next() {
		var p string
		if err := prows.Scan(&p); err != nil {
			lg.Error("role_permissions scan failed", slog.String("err", err.Error()))
			return "", nil, err
		}
		perms = append(perms, p)
	}
	return primary, perms, nil
}

// emitLoginFailure records a failed login attempt to the audit chain.
// userID is empty when the user could not be identified (wrong email).
// The audit emission is fire-and-forget — failures are logged but never
// propagate to the response.
func (a *API) emitLoginFailure(ctx context.Context, tenant, userID, email, reason string, r *http.Request) {
	if a.audit == nil {
		return
	}
	if err := a.audit.EmitEvent(ctx, audit.Event{
		TenantID:     tenant,
		ActorUser:    userID,
		Service:      "auth",
		Action:       "auth.login.failed",
		ResourceType: "session",
		ResourceID:   "",
		ActorIP:      clientIP(r),
		After: map[string]any{
			"email":      email,
			"reason":     reason,
			"user_agent": r.UserAgent(),
		},
	}); err != nil {
		a.log.Warn("audit emit (login.failed) failed",
			slog.String("err", err.Error()),
			slog.String("reason", reason))
	}
}

// emitLoginSuccess records a successful login to the audit chain.
// Same fire-and-forget contract as emitLoginFailure.
func (a *API) emitLoginSuccess(ctx context.Context, tenant, userID, email, role string, r *http.Request) {
	if a.audit == nil {
		return
	}
	if err := a.audit.EmitEvent(ctx, audit.Event{
		TenantID:     tenant,
		ActorUser:    userID,
		ActorRole:    role,
		Service:      "auth",
		Action:       "auth.login.success",
		ResourceType: "session",
		ResourceID:   userID,
		ActorIP:      clientIP(r),
		After: map[string]any{
			"email":      email,
			"user_agent": r.UserAgent(),
		},
	}); err != nil {
		a.log.Warn("audit emit (login.success) failed",
			slog.String("err", err.Error()))
	}
}

// clientIP extracts a best-effort client IP from the request. Behind a
// reverse proxy / load balancer (Fly's fly-edge, an nginx ingress, etc.),
// r.RemoteAddr is the proxy — the original client lives in X-Forwarded-For
// or X-Real-IP. We prefer the leftmost X-Forwarded-For entry (the original
// client), fall back to X-Real-IP, then to the connection peer.
//
// Forwarded headers can be spoofed by anything between the client and our
// trust boundary, so callers should treat this as "best effort for audit
// trails", not as an authentication signal.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-Ip"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type loggerCtxKey struct{}

// withLogger / loggerFromCtx let helper functions inherit the per-request
// logger (rid, tenant, email) without changing their public signatures.
func withLogger(ctx context.Context, lg *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, lg)
}
func loggerFromCtx(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok && v != nil {
		return v
	}
	return slog.Default()
}

func pickPrimary(roles []string) string {
	priority := []string{"admin", "court", "customs", "officer", "citizen"}
	for _, p := range priority {
		for _, r := range roles {
			if r == p {
				return p
			}
		}
	}
	return roles[0]
}
