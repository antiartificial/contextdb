//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpgradeExistingSchemaAddsLexicalIndex(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "contextdb_upgrade_" + uuid.NewString()[:8]
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for version, name := range []string{"001_initial.sql", "002_node_fingerprints.sql", "003_edge_versions.sql"} {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version+1); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGraphStore(pool)
	now := time.Now().Add(-time.Minute)
	node := core.Node{ID: uuid.New(), Namespace: schema, Properties: map[string]any{"text": "Historical INV-77 evidence"}, ValidFrom: now, TxTime: now}
	if err := g.UpsertNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	m := NewMigrator(pool)
	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	version, err := m.Version(ctx)
	if err != nil || version != 4 {
		t.Fatalf("migration version = %d, %v", version, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = $1 AND indexname = 'idx_nodes_lexical_trgm')`, schema).Scan(&exists); err != nil || !exists {
		t.Fatalf("lexical index missing: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes(id, namespace, properties, valid_from, tx_time)
		SELECT gen_random_uuid(), $1, jsonb_build_object('text', 'unrelated record ' || i), $2, $2
		FROM generate_series(1, 5000) AS i`, schema, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE nodes`); err != nil {
		t.Fatal(err)
	}
	planTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer planTx.Rollback(ctx)
	if _, err := planTx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := planTx.Query(ctx, `EXPLAIN SELECT id FROM nodes WHERE lower(CASE
		WHEN jsonb_typeof(properties->'text') = 'string' THEN properties->>'text'
		WHEN jsonb_typeof(properties->'content') = 'string' THEN properties->>'content'
		ELSE '' END) LIKE ANY($1)`, []string{"%inv-77%"})
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	rows.Close()
	if err := planTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_nodes_lexical_trgm") {
		var def string
		_ = pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = $1 AND indexname = 'idx_nodes_lexical_trgm'`, schema).Scan(&def)
		t.Fatalf("query plan does not use lexical index:\n%s\nindex: %s", plan.String(), def)
	}
	got, err := g.SearchText(ctx, store.TextQuery{Namespace: schema, Text: "INV-77", ValidAt: time.Now(), KnownAt: time.Now()})
	if err != nil || len(got) != 1 || got[0].Node.ID != node.ID {
		t.Fatalf("pre-upgrade node retrieval: %+v, %v", got, err)
	}
}
