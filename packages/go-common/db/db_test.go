package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// ctxWithClaims returns a context preloaded with claims for the
// supplied tenant + user. db.WithTenant requires this.
func ctxWithClaims(tenant, user, role string) context.Context {
	return auth.WithClaims(context.Background(), &auth.Claims{
		TenantID: tenant, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{Subject: user},
	})
}

// TestWithTenant_NoClaims: WithTenant returns an error if the ctx
// has no auth claims. This is the load-bearing guarantee that
// callers can't accidentally bypass tenant isolation.
func TestWithTenant_NoClaims(t *testing.T) {
	env := testkit.Setup(t)
	_, err := db.WithTenant(context.Background(), env.AdminPool())
	if err == nil {
		t.Fatal("WithTenant should error on context without claims")
	}
	if !errors.Is(err, err) || err.Error() == "" {
		t.Fatalf("err message: %v", err)
	}
}

// TestWithTenant_AppliesSessionVars: with claims in ctx, the returned
// Conn has app.tenant_id / app.user_id / app.role set. RLS policies
// use these vars; if WithTenant doesn't apply them, every query from
// a normal handler would see no rows.
func TestWithTenant_AppliesSessionVars(t *testing.T) {
	env := testkit.Setup(t)
	uid := uuid.NewString()
	ctx := ctxWithClaims(env.Tenant, uid, "admin")

	conn, err := db.WithTenant(ctx, env.AdminPool())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	var tenantVar, userVar, roleVar string
	if err := conn.QueryRow(ctx,
		`SELECT current_setting('app.tenant_id', true),
		        current_setting('app.user_id', true),
		        current_setting('app.role', true)`).
		Scan(&tenantVar, &userVar, &roleVar); err != nil {
		t.Fatal(err)
	}
	if tenantVar != env.Tenant {
		t.Errorf("tenant: %s", tenantVar)
	}
	if userVar != uid {
		t.Errorf("user: %s", userVar)
	}
	if roleVar != "admin" {
		t.Errorf("role: %s", roleVar)
	}
}

// TestRelease_ResetsSessionVars: after Release, the session vars are
// blank, so a subsequent Acquire of the same underlying connection
// from another request can't accidentally see the previous tenant.
func TestRelease_ResetsSessionVars(t *testing.T) {
	env := testkit.Setup(t)
	ctx := ctxWithClaims(env.Tenant, uuid.NewString(), "admin")

	conn, err := db.WithTenant(ctx, env.AdminPool())
	if err != nil {
		t.Fatal(err)
	}
	conn.Release()

	// Re-acquire raw and check the vars are blank.
	raw, _ := env.AdminPool().Acquire(ctx)
	defer raw.Release()
	var tenantVar string
	_ = raw.QueryRow(ctx,
		`SELECT current_setting('app.tenant_id', true)`).Scan(&tenantVar)
	if tenantVar != "" {
		t.Fatalf("tenant_id leaked across releases: %q", tenantVar)
	}
}

// TestInTx_CommitOnSuccess: InTx commits when the closure returns nil
// and rolls back when it returns an error. Tested via a side effect
// (a temp table modification) that survives commit but not rollback.
func TestInTx_CommitOnSuccess(t *testing.T) {
	env := testkit.Setup(t)
	ctx := ctxWithClaims(env.Tenant, uuid.NewString(), "admin")

	conn, err := db.WithTenant(ctx, env.AdminPool())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	// Use a tenant-scoped INSERT into vehicles (RLS-enabled in tests
	// when using the app pool — but env.AdminPool bypasses RLS, so
	// the insert just needs valid columns).
	plate := "DBT-" + uuid.NewString()[:6]

	if err := conn.InTx(ctx, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO vehicles (tenant_id, plate) VALUES ($1, $2)`,
			env.Tenant, plate)
		return e
	}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := env.QueryRow(
		`SELECT count(*) FROM vehicles WHERE plate=$1`, plate).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("commit didn't persist: %d rows", n)
	}
}

func TestInTx_RollbackOnError(t *testing.T) {
	env := testkit.Setup(t)
	ctx := ctxWithClaims(env.Tenant, uuid.NewString(), "admin")

	conn, err := db.WithTenant(ctx, env.AdminPool())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	plate := "RB-" + uuid.NewString()[:6]
	want := errors.New("intentional")
	got := conn.InTx(ctx, func(tx pgx.Tx) error {
		_, _ = tx.Exec(ctx,
			`INSERT INTO vehicles (tenant_id, plate) VALUES ($1, $2)`,
			env.Tenant, plate)
		return want
	})
	if !errors.Is(got, want) {
		t.Fatalf("err: %v", got)
	}

	var n int
	_ = env.QueryRow(
		`SELECT count(*) FROM vehicles WHERE plate=$1`, plate).Scan(&n)
	if n != 0 {
		t.Fatalf("rollback didn't undo: %d rows", n)
	}
}

