package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpsertNodeAndEdgeIfVersion commits the claim correction and audit edge in
// one SQL transaction. Any edge failure rolls the node version back too.
func (g *GraphStore) UpsertNodeAndEdgeIfVersion(ctx context.Context, n core.Node, expectedVersion uint64, edge core.Edge) (bool, error) {
	if n.ID == uuid.Nil || edge.ID == uuid.Nil || n.Namespace != edge.Namespace {
		return false, fmt.Errorf("atomic node/edge update requires IDs and one namespace")
	}
	if n.Labels == nil {
		n.Labels = []string{}
	}
	props, err := json.Marshal(n.Properties)
	if err != nil {
		return false, err
	}
	edgeProps, err := json.Marshal(edge.Properties)
	if err != nil {
		return false, err
	}
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "node/"+n.Namespace+"/"+n.ID.String()); err != nil {
		return false, err
	}
	var version uint64
	var latestTxTime time.Time
	if err := tx.QueryRow(ctx, `SELECT version, tx_time FROM nodes WHERE namespace = $1 AND id = $2 ORDER BY version DESC LIMIT 1`, n.Namespace, n.ID).Scan(&version, &latestTxTime); err == pgx.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if version != expectedVersion {
		return false, nil
	}
	if n.TxTime.IsZero() {
		n.TxTime = time.Now()
	}
	if !n.TxTime.After(latestTxTime) {
		n.TxTime = latestTxTime.Add(time.Microsecond)
	}
	if n.ValidFrom.IsZero() {
		n.ValidFrom = n.TxTime
	}
	edge.TxTime = n.TxTime
	if edge.ValidFrom.IsZero() {
		edge.ValidFrom = edge.TxTime
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO nodes (id, namespace, labels, properties, model_id, fingerprint, valid_from, valid_until, tx_time, confidence, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, n.ID, n.Namespace, n.Labels, props, n.ModelID, n.Fingerprint, n.ValidFrom, n.ValidUntil, n.TxTime, n.Confidence, expectedVersion+1); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO edges (id, namespace, src, dst, type, weight, properties, valid_from, valid_until, tx_time, invalidated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, edge.ID, edge.Namespace, edge.Src, edge.Dst, edge.Type, edge.Weight, edgeProps, edge.ValidFrom, edge.ValidUntil, edge.TxTime, edge.InvalidatedAt); err != nil {
		return false, err
	}
	if err := insertEdgeVersion(ctx, tx, edge.Namespace, edge.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
