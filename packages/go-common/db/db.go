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
// Always Release() it when done.
type Conn struct {
	*pgxpool.Conn
}

// WithTenant acquires a connection and applies the caller's tenant/user/role
// from the JWT claims as Postgres session variables.
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
		"SELECT set_config('app.tenant_id',$1,true), "+
			"set_config('app.user_id',$2,true), "+
			"set_config('app.role',$3,true)",
		c.TenantID, c.Subject, c.Role); err != nil {
		conn.Release()
		return nil, err
	}
	return &Conn{conn}, nil
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
