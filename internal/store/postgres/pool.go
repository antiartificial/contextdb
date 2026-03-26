// Package postgres provides store implementations backed by PostgreSQL with
// pgvector for ANN search and recursive CTEs for graph traversal.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a pgxpool connection pool.
type Pool struct {
	inner *pgxpool.Pool
}

// NewPool creates a connection pool from a DSN.
func NewPool(ctx context.Context, dsn string, maxConns int) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Pool{inner: pool}, nil
}

// Inner returns the underlying pgxpool.Pool.
func (p *Pool) Inner() *pgxpool.Pool { return p.inner }

// Close closes all connections in the pool.
func (p *Pool) Close() error {
	p.inner.Close()
	return nil
}
