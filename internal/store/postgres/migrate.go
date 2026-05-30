package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/antiartificial/contextdb/internal/buildinfo"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrator applies SQL migrations to the database.
type Migrator struct {
	pool *pgxpool.Pool
}

// NewMigrator returns a migrator for the given pool.
func NewMigrator(pool *pgxpool.Pool) *Migrator {
	return &Migrator{pool: pool}
}

// AvailableMigrations returns the embedded Postgres schema migrations.
func AvailableMigrations() []buildinfo.Migration {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	out := make([]buildinfo.Migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, label, ok := parseMigrationName(name)
		if !ok {
			continue
		}
		out = append(out, buildinfo.Migration{Version: version, Name: label})
	}
	return out
}

// Up applies all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	// ensure schema_migrations table
	if _, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	current, err := m.Version(ctx)
	if err != nil {
		return err
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, _, ok := parseMigrationName(name)
		if !ok {
			continue
		}
		if version <= current {
			continue
		}

		sql, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

func parseMigrationName(name string) (int, string, bool) {
	if len(name) < len("001_.sql") || !strings.HasSuffix(name, ".sql") {
		return 0, "", false
	}
	version, err := strconv.Atoi(name[:3])
	if err != nil || name[3] != '_' {
		return 0, "", false
	}
	label := strings.TrimSuffix(name[4:], ".sql")
	return version, label, true
}

// Version returns the highest applied migration version.
func (m *Migrator) Version(ctx context.Context) (int, error) {
	var v int
	err := m.pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	if err != nil {
		// table may not exist yet
		return 0, nil
	}
	return v, nil
}
