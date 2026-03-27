package federation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/antiartificial/contextdb/internal/snapshot"
	"github.com/antiartificial/contextdb/internal/store"
)

// Bootstrap handles initial sync for a new peer joining the federation.
type Bootstrap struct {
	graph  store.GraphStore
	vecs   store.VectorIndex
	log    store.EventLog
	logger *slog.Logger
}

// NewBootstrap creates a bootstrapper.
func NewBootstrap(graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, logger *slog.Logger) *Bootstrap {
	return &Bootstrap{graph: graph, vecs: vecs, log: log, logger: logger}
}

// ImportSnapshot imports an NDJSON snapshot for a namespace.
// This is the fast path for catching up a new peer — instead of replaying
// every event from epoch, import a snapshot then switch to incremental.
func (b *Bootstrap) ImportSnapshot(ctx context.Context, ns string, data []byte) error {
	importer := snapshot.NewImporter(b.graph, b.vecs)
	if err := importer.Import(ctx, ns, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("bootstrap import: %w", err)
	}
	b.logger.Info("bootstrap snapshot imported", "namespace", ns, "bytes", len(data))
	return nil
}

// ExportSnapshot exports the current state of a namespace as NDJSON.
// Used to serve snapshot requests from new peers.
func (b *Bootstrap) ExportSnapshot(ctx context.Context, ns string) ([]byte, error) {
	exporter := snapshot.NewExporter(b.graph)
	var buf bytes.Buffer
	if err := exporter.Export(ctx, ns, &buf); err != nil {
		return nil, fmt.Errorf("bootstrap export: %w", err)
	}
	return buf.Bytes(), nil
}
