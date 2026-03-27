package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// GraphStore implements store.GraphStore backed by PostgreSQL.
type GraphStore struct {
	pool *pgxpool.Pool
}

// NewGraphStore returns a GraphStore backed by the given connection pool.
func NewGraphStore(pool *pgxpool.Pool) *GraphStore {
	return &GraphStore{pool: pool}
}

func (g *GraphStore) UpsertNode(ctx context.Context, n core.Node) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.TxTime.IsZero() {
		n.TxTime = time.Now()
	}
	if n.ValidFrom.IsZero() {
		n.ValidFrom = n.TxTime
	}

	props, err := json.Marshal(n.Properties)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}

	_, err = g.pool.Exec(ctx, `
		INSERT INTO nodes (id, namespace, labels, properties, model_id, valid_from, valid_until, tx_time, confidence, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			COALESCE((SELECT MAX(version) FROM nodes WHERE id = $1 AND namespace = $2), 0) + 1
		)
	`, n.ID, n.Namespace, n.Labels, props, n.ModelID, n.ValidFrom, n.ValidUntil, n.TxTime, n.Confidence)
	return err
}

func (g *GraphStore) GetNode(ctx context.Context, ns string, id uuid.UUID) (*core.Node, error) {
	return g.scanNode(ctx, `
		SELECT id, namespace, labels, properties, model_id, valid_from, valid_until, tx_time, confidence, version
		FROM nodes WHERE namespace = $1 AND id = $2 ORDER BY version DESC LIMIT 1
	`, ns, id)
}

func (g *GraphStore) AsOf(ctx context.Context, ns string, id uuid.UUID, t time.Time) (*core.Node, error) {
	return g.scanNode(ctx, `
		SELECT id, namespace, labels, properties, model_id, valid_from, valid_until, tx_time, confidence, version
		FROM nodes
		WHERE namespace = $1 AND id = $2
		  AND valid_from <= $3
		  AND (valid_until IS NULL OR valid_until > $3)
		  AND tx_time <= $3
		ORDER BY version DESC LIMIT 1
	`, ns, id, t)
}

func (g *GraphStore) History(ctx context.Context, ns string, id uuid.UUID) ([]core.Node, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT id, namespace, labels, properties, model_id, valid_from, valid_until, tx_time, confidence, version
		FROM nodes WHERE namespace = $1 AND id = $2 ORDER BY version ASC
	`, ns, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []core.Node
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (g *GraphStore) UpsertEdge(ctx context.Context, e core.Edge) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}
	if e.ValidFrom.IsZero() {
		e.ValidFrom = e.TxTime
	}

	props, err := json.Marshal(e.Properties)
	if err != nil {
		return fmt.Errorf("marshal edge properties: %w", err)
	}

	_, err = g.pool.Exec(ctx, `
		INSERT INTO edges (id, namespace, src, dst, type, weight, properties, valid_from, valid_until, tx_time, invalidated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			weight = EXCLUDED.weight,
			properties = EXCLUDED.properties,
			invalidated_at = EXCLUDED.invalidated_at
	`, e.ID, e.Namespace, e.Src, e.Dst, e.Type, e.Weight, props,
		e.ValidFrom, e.ValidUntil, e.TxTime, e.InvalidatedAt)
	return err
}

func (g *GraphStore) InvalidateEdge(ctx context.Context, ns string, id uuid.UUID, at time.Time) error {
	tag, err := g.pool.Exec(ctx, `
		UPDATE edges SET invalidated_at = $1 WHERE id = $2 AND namespace = $3
	`, at, id, ns)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("edge %s not found in namespace %s", id, ns)
	}
	return nil
}

func (g *GraphStore) GetEdges(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesFrom(ctx, ns, nodeID, nil)
}

func (g *GraphStore) GetEdgesTo(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error) {
	return g.EdgesTo(ctx, ns, nodeID, nil)
}

func (g *GraphStore) EdgesFrom(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	return g.queryEdges(ctx, ns, nodeID, edgeTypes, "src")
}

func (g *GraphStore) EdgesTo(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error) {
	return g.queryEdges(ctx, ns, nodeID, edgeTypes, "dst")
}

func (g *GraphStore) queryEdges(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string, direction string) ([]core.Edge, error) {
	query := fmt.Sprintf(`
		SELECT id, namespace, src, dst, type, weight, properties, valid_from, valid_until, tx_time, invalidated_at
		FROM edges
		WHERE namespace = $1 AND %s = $2
		  AND invalidated_at IS NULL
		  AND valid_from <= now()
		  AND (valid_until IS NULL OR valid_until > now())
	`, direction)

	var rows pgx.Rows
	var err error
	if len(edgeTypes) > 0 {
		query += " AND type = ANY($3)"
		rows, err = g.pool.Query(ctx, query, ns, nodeID, edgeTypes)
	} else {
		rows, err = g.pool.Query(ctx, query, ns, nodeID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []core.Edge
	for rows.Next() {
		e, err := scanEdgeRow(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func (g *GraphStore) Walk(ctx context.Context, q store.WalkQuery) ([]core.Node, error) {
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	maxDepth := q.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	query := `
		WITH RECURSIVE walk AS (
			SELECT n.id, n.namespace, n.labels, n.properties, n.model_id,
				   n.valid_from, n.valid_until, n.tx_time, n.confidence, n.version,
				   0 AS depth
			FROM nodes n
			WHERE n.namespace = $1
			  AND n.id = ANY($2)
			  AND n.valid_from <= $3
			  AND (n.valid_until IS NULL OR n.valid_until > $3)
			  AND n.version = (SELECT MAX(version) FROM nodes WHERE id = n.id AND namespace = n.namespace)

			UNION

			SELECT n2.id, n2.namespace, n2.labels, n2.properties, n2.model_id,
				   n2.valid_from, n2.valid_until, n2.tx_time, n2.confidence, n2.version,
				   w.depth + 1
			FROM walk w
			JOIN edges e ON e.namespace = $1 AND e.src = w.id
				AND e.invalidated_at IS NULL
				AND e.valid_from <= $3
				AND (e.valid_until IS NULL OR e.valid_until > $3)
				AND ($4::text[] IS NULL OR e.type = ANY($4))
				AND e.weight >= $5
			JOIN nodes n2 ON n2.namespace = $1 AND n2.id = e.dst
				AND n2.valid_from <= $3
				AND (n2.valid_until IS NULL OR n2.valid_until > $3)
				AND n2.version = (SELECT MAX(version) FROM nodes WHERE id = n2.id AND namespace = n2.namespace)
			WHERE w.depth < $6
		)
		SELECT DISTINCT ON (id) id, namespace, labels, properties, model_id,
			   valid_from, valid_until, tx_time, confidence, version
		FROM walk ORDER BY id, depth
	`

	var edgeTypes []string
	if len(q.EdgeTypes) > 0 {
		edgeTypes = q.EdgeTypes
	}

	rows, err := g.pool.Query(ctx, query, q.Namespace, q.SeedIDs, asOf, edgeTypes, q.MinWeight, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []core.Node
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (g *GraphStore) RetractNode(ctx context.Context, ns string, id uuid.UUID, reason string, at time.Time) error {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE nodes SET valid_until = $1 WHERE namespace = $2 AND id = $3 AND valid_until IS NULL
	`, at, ns, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node %s not found in namespace %s", id, ns)
	}

	// Create retraction edge via the pool (outside tx) would skip the tx,
	// so insert directly within the transaction.
	retractionEdge := core.Edge{
		ID:         uuid.New(),
		Namespace:  ns,
		Src:        id,
		Dst:        id,
		Type:       "retracted",
		Weight:     1.0,
		Properties: map[string]any{"reason": reason},
		ValidFrom:  at,
		TxTime:     at,
	}

	props, err := json.Marshal(retractionEdge.Properties)
	if err != nil {
		return fmt.Errorf("marshal edge properties: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO edges (id, namespace, src, dst, type, weight, properties, valid_from, valid_until, tx_time, invalidated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			weight = EXCLUDED.weight,
			properties = EXCLUDED.properties,
			invalidated_at = EXCLUDED.invalidated_at
	`, retractionEdge.ID, retractionEdge.Namespace, retractionEdge.Src, retractionEdge.Dst,
		retractionEdge.Type, retractionEdge.Weight, props,
		retractionEdge.ValidFrom, retractionEdge.ValidUntil, retractionEdge.TxTime, retractionEdge.InvalidatedAt)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (g *GraphStore) UpsertSource(ctx context.Context, s core.Source) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	s.UpdatedAt = time.Now()

	_, err := g.pool.Exec(ctx, `
		INSERT INTO sources (id, namespace, external_id, labels, credibility_score,
			claims_asserted, claims_validated, claims_refuted, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (namespace, external_id) DO UPDATE SET
			labels = EXCLUDED.labels,
			credibility_score = EXCLUDED.credibility_score,
			claims_asserted = EXCLUDED.claims_asserted,
			claims_validated = EXCLUDED.claims_validated,
			claims_refuted = EXCLUDED.claims_refuted,
			updated_at = EXCLUDED.updated_at
	`, s.ID, s.Namespace, s.ExternalID, s.Labels, s.Alpha, s.Beta,
		s.ClaimsAsserted, s.ClaimsValidated, s.ClaimsRefuted, s.CreatedAt, s.UpdatedAt)
	return err
}

func (g *GraphStore) GetSourceByExternalID(ctx context.Context, ns, externalID string) (*core.Source, error) {
	row := g.pool.QueryRow(ctx, `
		SELECT id, namespace, external_id, labels, credibility_score,
			claims_asserted, claims_validated, claims_refuted, created_at, updated_at
		FROM sources WHERE namespace = $1 AND external_id = $2
	`, ns, externalID)

	var s core.Source
	err := row.Scan(&s.ID, &s.Namespace, &s.ExternalID, &s.Labels, &s.Alpha, &s.Beta,
		&s.ClaimsAsserted, &s.ClaimsValidated, &s.ClaimsRefuted, &s.CreatedAt, &s.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (g *GraphStore) UpdateCredibility(ctx context.Context, ns string, id uuid.UUID, delta float64) error {
	tag, err := g.pool.Exec(ctx, `
		UPDATE sources
		SET credibility_score = GREATEST(0, LEAST(1, credibility_score + $1)),
			updated_at = now()
		WHERE namespace = $2 AND id = $3
	`, delta, ns, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("source %s not found", id)
	}
	return nil
}

func (g *GraphStore) Diff(ctx context.Context, ns string, t1, t2 time.Time) ([]store.NodeDiff, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT id, namespace, labels, properties, model_id, valid_from, valid_until, tx_time, confidence, version
		FROM nodes
		WHERE namespace = $1 AND tx_time > $2 AND tx_time <= $3
		ORDER BY tx_time
	`, ns, t1, t2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var diffs []store.NodeDiff
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		change := store.DiffAdded
		if n.Version > 1 {
			change = store.DiffModified
		}
		if n.ValidUntil != nil && !n.ValidUntil.After(t2) {
			change = store.DiffRemoved
		}
		diffs = append(diffs, store.NodeDiff{Node: n, Change: change})
	}
	return diffs, rows.Err()
}

func (g *GraphStore) ValidAt(ctx context.Context, ns string, t time.Time, labels []string) ([]core.Node, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT DISTINCT ON (id) id, namespace, labels, properties, model_id,
		       valid_from, valid_until, tx_time, confidence, version
		FROM nodes
		WHERE namespace = $1
		  AND valid_from <= $2
		  AND (valid_until IS NULL OR valid_until > $2)
		ORDER BY id, version DESC
	`, ns, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []core.Node
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		if len(labels) > 0 {
			skip := false
			for _, l := range labels {
				if !n.HasLabel(l) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// ─── scan helpers ─────────────────────────────────────────────────────────────

func (g *GraphStore) scanNode(ctx context.Context, query string, args ...any) (*core.Node, error) {
	row := g.pool.QueryRow(ctx, query, args...)
	n, err := scanSingleNodeRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func scanSingleNodeRow(row pgx.Row) (core.Node, error) {
	var n core.Node
	var props []byte
	err := row.Scan(&n.ID, &n.Namespace, &n.Labels, &props, &n.ModelID,
		&n.ValidFrom, &n.ValidUntil, &n.TxTime, &n.Confidence, &n.Version)
	if err != nil {
		return n, err
	}
	if len(props) > 0 {
		if err := json.Unmarshal(props, &n.Properties); err != nil {
			return n, err
		}
	}
	if n.Properties == nil {
		n.Properties = make(map[string]any)
	}
	return n, nil
}

func scanNodeRow(rows pgx.Rows) (core.Node, error) {
	var n core.Node
	var props []byte
	err := rows.Scan(&n.ID, &n.Namespace, &n.Labels, &props, &n.ModelID,
		&n.ValidFrom, &n.ValidUntil, &n.TxTime, &n.Confidence, &n.Version)
	if err != nil {
		return n, err
	}
	if len(props) > 0 {
		if err := json.Unmarshal(props, &n.Properties); err != nil {
			return n, err
		}
	}
	if n.Properties == nil {
		n.Properties = make(map[string]any)
	}
	return n, nil
}

func scanEdgeRow(rows pgx.Rows) (core.Edge, error) {
	var e core.Edge
	var props []byte
	err := rows.Scan(&e.ID, &e.Namespace, &e.Src, &e.Dst, &e.Type, &e.Weight,
		&props, &e.ValidFrom, &e.ValidUntil, &e.TxTime, &e.InvalidatedAt)
	if err != nil {
		return e, err
	}
	if len(props) > 0 {
		if err := json.Unmarshal(props, &e.Properties); err != nil {
			return e, err
		}
	}
	if e.Properties == nil {
		e.Properties = make(map[string]any)
	}
	return e, nil
}
