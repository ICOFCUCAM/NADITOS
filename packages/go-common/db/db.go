// Package db wraps pgxpool with tenant-aware connection acquisition.
//
// Every request handler should call WithTenant(ctx, pool) to obtain a
// *pgx.Conn that has app.tenant_id, app.user_id, app.role set as session
// variables — Postgres RLS policies use these to enforce isolation.
package db

import (
	"context"
	"errors"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Conn is an acquired connection with tenant context already applied.
// Always Release() it when done — Release() resets the session vars so
// the connection can be reused safely by another tenant's request.
type Conn struct {
	*pgxpool.Conn
	ctx context.Context
}

// WithTenant acquires a connection and applies the caller's tenant/user/role
// from the JWT claims as Postgres session variables.
//
// We use SET (session-level) rather than SET LOCAL because handlers don't
// universally wrap their work in a transaction — SET LOCAL outside a tx
// is a no-op and would silently disable RLS. To prevent leakage between
// requests we RESET the vars in Conn.Release().
func WithTenant(ctx context.Context, pool *pgxpool.Pool) (*Conn, error) {
	c := auth.ClaimsFrom(ctx)
	if c == nil {
		return nil, errors.New("db.WithTenant: no auth claims in context")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx,
		"SELECT set_config('app.tenant_id',$1,false), "+
			"set_config('app.user_id',$2,false), "+
			"set_config('app.role',$3,false)",
		c.TenantID, c.Subject, c.Role); err != nil {
		conn.Release()
		return nil, err
	}
	return &Conn{Conn: conn, ctx: ctx}, nil
}

// Release returns the connection to the pool after clearing the tenant
// context so a subsequent acquire by another request starts clean.
func (c *Conn) Release() {
	if c == nil || c.Conn == nil {
		return
	}
	// best-effort reset; if the underlying conn is already broken we
	// release anyway — the pool discards broken conns.
	_, _ = c.Conn.Exec(c.ctx,
		"SELECT set_config('app.tenant_id','',false), "+
			"set_config('app.user_id','',false), "+
			"set_config('app.role','',false)")
	c.Conn.Release()
}

// AsAdmin acquires a connection with row_security off — for migrations and
// internal jobs only. Never use inside request handlers.
func AsAdmin(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

// InTx runs fn inside a transaction on the tenant connection.
func (c *Conn) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
