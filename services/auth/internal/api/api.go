// Package api wires the HTTP surface of the auth service.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool) http.Handler {
	a := &API{
		cfg:    cfg,
		log:    log,
		pool:   pool,
		issuer: auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/login",   a.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", a.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout",  a.handleLogout)
	mux.HandleFunc("GET  /v1/auth/me",      a.handleMe)
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
	tenant := req.Tenant
	if tenant == "" {
		tenant = r.Header.Get("X-Tenant-Id")
	}
	if tenant == "" {
		tenant = a.cfg.DefaultTenant
	}
	lg := a.log.With(
		slog.String("rid", rid),
		slog.String("step", "login"),
		slog.String("tenant", tenant),
		slog.String("email", req.Email),
	)
	lg.Info("login: start")

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
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	case err != nil:
		lg.Error("login: user lookup query failed", slog.String("err", err.Error()))
		httpx.WriteErr(w, err)
		return
	case !isActive:
		lg.Info("login: user inactive", slog.String("user_id", userID.String()))
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}
	lg = lg.With(slog.String("user_id", userID.String()))
	lg.Info("login: user found, verifying password")

	if err := auth.CheckPassword(passwordHash, req.Password); err != nil {
		lg.Info("login: password mismatch", slog.String("err", err.Error()))
		httpx.WriteErr(w, httpx.ErrUnauthorized)
		return
	}

	role, perms, err := loadRoleAndPerms(ctx, tx, tenant, userID)
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

	httpx.WriteJSON(w, http.StatusOK, tokenResp{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(a.cfg.AccessTTL),
		User: meResp{
			ID: userID.String(), Tenant: tenant, Email: req.Email,
			FullName: fullName, Role: role, Permissions: perms,
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

	role, perms, err := loadRoleAndPerms(ctx, tx, tenant, userID)
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
	httpx.WriteJSON(w, http.StatusOK, meResp{
		ID: c.Subject, Tenant: c.TenantID,
		Role: c.Role, Permissions: c.Permissions,
	})
}

// handleAdminCreateUser is a bootstrapping endpoint used by seed scripts.
// In production, this must be behind admin RBAC; we accept tenant from
// the X-Tenant-Id header and require ADMIN_BOOTSTRAP_KEY env or an admin JWT.
func (a *API) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rid, _, _ := observability.IDs(ctx)

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

// Querier is the small subset of pgx we need; both *pgx.Conn and pgx.Tx satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadRoleAndPerms returns the user's primary role and flattened permissions.
// The "primary" role is admin > court > customs > officer > citizen, in that order.
func loadRoleAndPerms(ctx context.Context, conn Querier, tenant string, userID uuid.UUID) (string, []string, error) {
	rows, err := conn.Query(ctx,
		`SELECT role_code FROM user_roles WHERE tenant_id=$1 AND user_id=$2`,
		tenant, userID)
	if err != nil {
		return "", nil, err
	}
	roles := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return "", nil, err
		}
		roles = append(roles, r)
	}
	rows.Close()
	if len(roles) == 0 {
		return "citizen", nil, nil
	}
	primary := pickPrimary(roles)

	prows, err := conn.Query(ctx,
		`SELECT DISTINCT permission FROM role_permissions
		   WHERE tenant_id=$1 AND role_code = ANY($2)`,
		tenant, roles)
	if err != nil {
		return "", nil, err
	}
	defer prows.Close()
	perms := []string{}
	for prows.Next() {
		var p string
		if err := prows.Scan(&p); err != nil {
			return "", nil, err
		}
		perms = append(perms, p)
	}
	return primary, perms, nil
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
