// Package testkit boots integration tests against a real Postgres.
//
// Tests opt in via the TEST_DATABASE_URL env var; without it the tests
// SKIP rather than fail, so `go test ./...` keeps working without infra.
//
// Conventions:
//   - Each test calls testkit.Setup(t) which returns a unique tenant id
//     so concurrent tests don't collide.
//   - All migrations under db/migrations/ are applied idempotently the
//     first time any test runs in the process.
//   - testkit mints JWTs for any role and seeds the tenant + roles +
//     permissions + offences so the handlers under test can authorize.
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type pgxConn = pgx.Conn

// Env is the integration-test fixture handed to every test.
//
//	Pool       — the "app" pool, used by handlers. Subject to RLS.
//	adminPool  — bypasses RLS for fixture writes (kept package-private).
type Env struct {
	T         *testing.T
	Ctx       context.Context
	Pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
	Issuer    *auth.Issuer
	Tenant    string
	Cfg       config.Service
}

// Token mints a signed JWT for a fresh user in this test's tenant. The
// user row is created with a per-test email so foreign keys (e.g.
// fines.issued_by → users.id) hold.
func (e *Env) Token(role string, perms ...string) (token, userID string) {
	uid := uuid.New()
	e.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', $4)
	         ON CONFLICT (tenant_id, email) DO NOTHING`,
		uid, e.Tenant, fmt.Sprintf("%s@%s", uid.String()[:8], e.Tenant),
		fmt.Sprintf("Test %s", role))
	if role != "" {
		e.Exec(`INSERT INTO user_roles (tenant_id, user_id, role_code) VALUES ($1,$2,$3)
		         ON CONFLICT DO NOTHING`, e.Tenant, uid, role)
	}
	tok, err := e.Issuer.Sign(uid, auth.Claims{
		TenantID: e.Tenant, Role: role, Permissions: perms,
	})
	if err != nil {
		e.T.Fatalf("sign jwt: %v", err)
	}
	return tok, uid.String()
}

// Req builds an authenticated HTTP request bound to the test's tenant.
func (e *Env) Req(method, path, body string, token string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", e.Tenant)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// Exec runs sql via the admin pool (BYPASSRLS) and Fatals on error —
// for fixture setup. Never use for handler queries.
func (e *Env) Exec(sql string, args ...any) {
	if _, err := e.adminPool.Exec(e.Ctx, sql, args...); err != nil {
		e.T.Fatalf("exec %q: %v", sql, err)
	}
}

// AdminExec runs sql via the admin pool and returns the error to the
// caller. Use when the call is *expected* to fail (e.g. asserting that
// a tamper-detection trigger fires).
func (e *Env) AdminExec(sql string, args ...any) (any, error) {
	tag, err := e.adminPool.Exec(e.Ctx, sql, args...)
	return tag, err
}

// AdminPool exposes the BYPASSRLS pool. Used by tests that exercise
// services which legitimately operate across tenants (audit, migrations,
// outbox relay) — those services run with a privileged role in production.
func (e *Env) AdminPool() *pgxpool.Pool { return e.adminPool }

// QueryRow runs a query via the admin pool.
type Row struct {
	scan func(...any) error
}

func (r *Row) Scan(dest ...any) error { return r.scan(dest...) }

func (e *Env) QueryRow(sql string, args ...any) *Row {
	row := e.adminPool.QueryRow(e.Ctx, sql, args...)
	return &Row{scan: row.Scan}
}

// ─── Process-wide bootstrap (migrations + pools) ────────────────────────────
//
// Two pools share the same DB:
//
//	adminPool  — connects as the BYPASSRLS migrations role; used to apply
//	             migrations and write fixtures across tenants.
//	appPool    — connects as the non-bypass app role; every connection
//	             SETs row_security=on so RLS is enforced exactly as in
//	             production. Tested code paths use this.
//
// We provision the app role on first boot if it doesn't exist.
var (
	bootOnce      sync.Once
	bootAppPool   *pgxpool.Pool
	bootAdminPool *pgxpool.Pool
	bootErr       error
	bootCfg       config.Service
)

const testAppRole = "naditos_app"

func dbURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return os.Getenv("DATABASE_URL")
}

func boot(t *testing.T) (appPool, adminPool *pgxpool.Pool, cfg config.Service) {
	bootOnce.Do(func() {
		url := dbURL()
		if url == "" {
			bootErr = fmt.Errorf("TEST_DATABASE_URL not set")
			return
		}
		_ = os.Setenv("DATABASE_URL", url)
		_ = os.Setenv("JWT_SECRET", "test-secret-do-not-use-in-prod-test-secret-do-not-use-in-prod")
		bootCfg = config.MustLoad("test", 0)
		httpx.DebugErrors = true

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		admin, err := db.Open(ctx, url)
		if err != nil {
			bootErr = err
			return
		}
		bootAdminPool = admin
		if err := applyMigrations(ctx, admin); err != nil {
			bootErr = err
			return
		}
		if err := provisionAppRole(ctx, admin); err != nil {
			bootErr = fmt.Errorf("provision app role: %w", err)
			return
		}

		// App pool — every new connection switches into the non-bypass
		// app role so RLS is enforced.
		cfg2, err := pgxpool.ParseConfig(url)
		if err != nil {
			bootErr = err
			return
		}
		cfg2.AfterConnect = func(ctx context.Context, c *pgxConn) error {
			_, err := c.Exec(ctx, "SET ROLE "+testAppRole)
			return err
		}
		app, err := pgxpool.NewWithConfig(ctx, cfg2)
		if err != nil {
			bootErr = err
			return
		}
		bootAppPool = app
	})
	if bootErr != nil {
		t.Skipf("integration tests skipped: %v", bootErr)
	}
	return bootAppPool, bootAdminPool, bootCfg
}

// provisionAppRole verifies the non-BYPASSRLS role used by test handlers
// is present and grantable. The role itself must be created out of band
// (CI provisioning script / docker entrypoint) because CREATE ROLE
// requires CREATEROLE privilege on the connecting user.
func provisionAppRole(ctx context.Context, admin *pgxpool.Pool) error {
	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=$1)`, testAppRole).
		Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("role %q must be created before tests run "+
			"(see scripts/provision_test_role.sql)", testAppRole)
	}
	// Idempotent privilege grants — these only fail if naditos_app
	// is already privileged, which is fine.
	for _, s := range []string{
		`GRANT USAGE ON SCHEMA public TO ` + testAppRole,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ` + testAppRole,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + testAppRole,
		`GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO ` + testAppRole,
	} {
		if _, err := admin.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", strings.SplitN(s, "\n", 2)[0], err)
		}
	}
	return nil
}

// Setup returns a fresh fixture for one test.
//
// Each call provisions a unique tenant id (`t_<rand>`) and seeds it with
// roles, default permissions, and the demo regulation_offences set so
// handlers find the rows they expect. Test isolation comes from the
// tenant id — RLS keeps tests from seeing each other's rows.
func Setup(t *testing.T) *Env {
	t.Helper()
	app, admin, cfg := boot(t)
	tenant := "t_" + randHex(6)
	env := &Env{
		T: t, Ctx: context.Background(),
		Pool: app, adminPool: admin, Tenant: tenant,
		Issuer: auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL),
		Cfg:    cfg,
	}
	seedTenant(t, env)
	t.Cleanup(func() { teardownTenant(env) })
	return env
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// seedTenant writes the minimal rows every test relies on.
func seedTenant(t *testing.T, e *Env) {
	t.Helper()
	e.Exec(`INSERT INTO tenants (id, name, country_code, default_locale, currency, plate_regex)
	         VALUES ($1, 'Test Tenant', 'XX', 'en', 'EUR', '^[A-Z0-9-]{2,10}$')
	         ON CONFLICT (id) DO NOTHING`, e.Tenant)

	for _, r := range []struct{ Code, Name string }{
		{"admin", "Administrator"}, {"officer", "Officer"},
		{"citizen", "Citizen"}, {"court", "Court"},
	} {
		e.Exec(`INSERT INTO roles (tenant_id, code, name) VALUES ($1,$2,$3)
		         ON CONFLICT DO NOTHING`, e.Tenant, r.Code, r.Name)
	}
	for _, p := range []struct{ Role, Perm string }{
		{"admin", "registry:read"}, {"admin", "registry:write"},
		{"admin", "fines:read"}, {"admin", "fines:cancel"},
		{"admin", "license:read"}, {"admin", "license:write"},
		{"admin", "audit:read"}, {"admin", "anpr:scan"},
		{"officer", "registry:read"}, {"officer", "fines:create"},
		{"officer", "fines:read"}, {"officer", "anpr:scan"},
		{"officer", "license:read"},
		{"citizen", "fines:pay"}, {"citizen", "fines:dispute"},
	} {
		e.Exec(`INSERT INTO role_permissions (tenant_id, role_code, permission)
		        VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, e.Tenant, p.Role, p.Perm)
	}

	// Minimal regulation set so fines tests have something to price against.
	for _, o := range []struct {
		Code   string
		Amount string
		Window int
		Points int
	}{
		{"INS_EXPIRED", "400.00", 1440, 4},
		{"INSP_EXPIRED", "200.00", 1440, 2},
		{"SPEED_30", "500.00", 60, 6},
	} {
		e.Exec(`INSERT INTO regulation_offences
		         (tenant_id, code, name, base_amount, currency, duplicate_window_min, is_active)
		         VALUES ($1, $2::text, jsonb_build_object('en', $2::text), $3::numeric, 'EUR', $4::int, true)
		         ON CONFLICT DO NOTHING`,
			e.Tenant, o.Code, o.Amount, o.Window)
	}
	// Bind a country pack matching the manifest contract: offences is
	// a JSON array of objects with a `code` field (regulation.Pack
	// uses []Offence, so production demo + tests stay aligned).
	e.Exec(`INSERT INTO country_packs (id, country_code, version, effective_from, manifest)
	         VALUES ($1::text, 'XX', '1.0', '2026-01-01',
	           jsonb_build_object('id', $1::text, 'country_code','XX','version','1.0',
	             'plate_regex','^[A-Z0-9-]{2,10}$', 'currency','EUR',
	             'offences', jsonb_build_array(
	                jsonb_build_object('code','INS_EXPIRED',  'points', 4),
	                jsonb_build_object('code','INSP_EXPIRED', 'points', 2),
	                jsonb_build_object('code','SPEED_30',     'points', 6))))
	         ON CONFLICT (id) DO NOTHING`, "pack_"+e.Tenant)
	e.Exec(`INSERT INTO tenant_country_pack (tenant_id, pack_id) VALUES ($1, $2)
	         ON CONFLICT (tenant_id) DO UPDATE SET pack_id=EXCLUDED.pack_id`,
		e.Tenant, "pack_"+e.Tenant)
	e.Exec(`INSERT INTO driver_demerit_policy (tenant_id, threshold_points, window_months, suspension_months, reset_after_months)
	         VALUES ($1, 12, 24, 6, 36)
	         ON CONFLICT (tenant_id) DO NOTHING`, e.Tenant)
}

func teardownTenant(e *Env) {
	// CASCADE on tenants → drops every dependent row.
	_, _ = e.adminPool.Exec(e.Ctx, `DELETE FROM tenants WHERE id=$1`, e.Tenant)
	_, _ = e.adminPool.Exec(e.Ctx, `DELETE FROM country_packs WHERE id=$1`, "pack_"+e.Tenant)
}

// ─── Migration runner (mirrors scripts/migrate.sh up) ───────────────────────
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	dir, err := repoMigrationsDir()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var ups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	// ReadDir returns sorted; rely on numeric prefix ordering.
	for _, name := range ups {
		version := strings.TrimSuffix(name, ".up.sql")
		var seen int
		_ = conn.QueryRow(ctx, `SELECT 1 FROM schema_migrations WHERE version=$1`, version).Scan(&seen)
		if seen == 1 {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
			return err
		}
	}
	return nil
}

// repoMigrationsDir walks up from this source file to find db/migrations.
// Robust to the test being run from any service or package directory.
func repoMigrationsDir() (string, error) {
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "db", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("testkit: db/migrations not found")
}
