package client

import (
	"context"
	"fmt"
	"github.com/antiartificial/contextdb/internal/snapshot"
	"github.com/google/uuid"
	"io"
)

// ExportSnapshot writes a namespace snapshot as NDJSON.
func (db *DB) ExportSnapshot(ctx context.Context, namespace string, w io.Writer) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return fmt.Errorf("contextdb: connection closed")
	}
	return snapshot.NewExporter(db.graph).Export(ctx, namespace, w)
}

// ExportSnapshotFromSeeds writes a seeded namespace snapshot as NDJSON.
func (db *DB) ExportSnapshotFromSeeds(ctx context.Context, namespace string, seedIDs []uuid.UUID, maxDepth int, w io.Writer) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return fmt.Errorf("contextdb: connection closed")
	}
	return snapshot.NewExporter(db.graph).ExportFromSeeds(ctx, namespace, seedIDs, maxDepth, w)
}

// ImportSnapshot imports an NDJSON snapshot into namespace.
func (db *DB) ImportSnapshot(ctx context.Context, namespace string, r io.Reader) error {
	_, err := db.ImportSnapshotReport(ctx, namespace, r)
	return err
}

// ImportSnapshotReport imports an NDJSON snapshot and returns processed counts.
func (db *DB) ImportSnapshotReport(ctx context.Context, namespace string, r io.Reader) (SnapshotReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return SnapshotReport{}, fmt.Errorf("contextdb: connection closed")
	}
	report, err := snapshot.NewImporter(db.graph, db.vecs).ImportWithReport(ctx, namespace, r)
	return snapshotReport(report, false), err
}

// ValidateSnapshot verifies an NDJSON snapshot without writing to the DB.
func (db *DB) ValidateSnapshot(ctx context.Context, namespace string, r io.Reader) error {
	_, err := db.ValidateSnapshotReport(ctx, namespace, r)
	return err
}

// ValidateSnapshotReport verifies an NDJSON snapshot without writing and returns processed counts.
func (db *DB) ValidateSnapshotReport(ctx context.Context, namespace string, r io.Reader) (SnapshotReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return SnapshotReport{}, fmt.Errorf("contextdb: connection closed")
	}
	report, err := snapshot.NewImporter(db.graph, db.vecs).ValidateWithReport(ctx, namespace, r)
	return snapshotReport(report, true), err
}

func snapshotReport(report snapshot.ImportReport, dryRun bool) SnapshotReport {
	return SnapshotReport{
		Namespace:          report.Namespace,
		DryRun:             dryRun,
		Lines:              report.Lines,
		Nodes:              report.Nodes,
		Edges:              report.Edges,
		Sources:            report.Sources,
		Vectors:            report.Vectors,
		NamespaceOverrides: report.NamespaceOverrides,
		NewNodes:           report.NewNodes,
		ChangedNodes:       report.ChangedNodes,
		UnchangedNodes:     report.UnchangedNodes,
	}
}

// SnapshotReport summarizes records processed during snapshot import.
type SnapshotReport struct {
	Namespace          string `json:"namespace"`
	DryRun             bool   `json:"dry_run"`
	Lines              int    `json:"lines"`
	Nodes              int    `json:"nodes"`
	Edges              int    `json:"edges"`
	Sources            int    `json:"sources"`
	Vectors            int    `json:"vectors"`
	NamespaceOverrides int    `json:"namespace_overrides"`
	NewNodes           int    `json:"new_nodes"`
	ChangedNodes       int    `json:"changed_nodes"`
	UnchangedNodes     int    `json:"unchanged_nodes"`
}
