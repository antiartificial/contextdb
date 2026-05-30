// Package snapshot provides NDJSON export and import for contextdb namespaces.
//
// The snapshot format is newline-delimited JSON. Each line contains a JSON
// object with a "type" field indicating the record type: "node", "edge", or
// "source". Nodes are written first, then edges, then sources.
//
// Example:
//
//	{"type":"node","data":{...}}
//	{"type":"edge","data":{...}}
//	{"type":"source","data":{...}}
package snapshot

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// recordType identifies the kind of record in the snapshot.
type recordType string

const (
	recordNode   recordType = "node"
	recordEdge   recordType = "edge"
	recordSource recordType = "source"
)

// record is a single NDJSON line in the snapshot.
type record struct {
	Type recordType      `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Exporter writes namespace data to an NDJSON stream.
type Exporter struct {
	graph store.GraphStore
}

// NewExporter creates a new Exporter that reads from the given graph store.
func NewExporter(graph store.GraphStore) *Exporter {
	return &Exporter{graph: graph}
}

// Export writes all current nodes, their history, outgoing edges, and sources
// for the given namespace to w in NDJSON format.
func (e *Exporter) Export(ctx context.Context, namespace string, w io.Writer) error {
	type sourceIterator interface {
		AllSources(ctx context.Context, ns string) ([]core.Source, error)
	}

	enc := json.NewEncoder(w)
	nodes, err := e.graph.ValidAt(ctx, namespace, time.Now(), nil)
	if err != nil {
		return fmt.Errorf("export nodes: %w", err)
	}
	exportedEdges := make(map[uuid.UUID]bool)

	for _, n := range nodes {
		versions, err := e.graph.History(ctx, namespace, n.ID)
		if err != nil {
			return fmt.Errorf("history %s: %w", n.ID, err)
		}
		for _, version := range versions {
			data, err := json.Marshal(version)
			if err != nil {
				return fmt.Errorf("marshal node %s: %w", version.ID, err)
			}
			if err := enc.Encode(record{Type: recordNode, Data: data}); err != nil {
				return fmt.Errorf("encode node %s: %w", version.ID, err)
			}
		}
		edges, err := e.graph.EdgesFrom(ctx, namespace, n.ID, nil)
		if err != nil {
			return fmt.Errorf("edges from %s: %w", n.ID, err)
		}
		for _, edge := range edges {
			if exportedEdges[edge.ID] {
				continue
			}
			exportedEdges[edge.ID] = true
			data, err := json.Marshal(edge)
			if err != nil {
				return fmt.Errorf("marshal edge %s: %w", edge.ID, err)
			}
			if err := enc.Encode(record{Type: recordEdge, Data: data}); err != nil {
				return fmt.Errorf("encode edge %s: %w", edge.ID, err)
			}
		}
	}

	if si, ok := e.graph.(sourceIterator); ok {
		sources, err := si.AllSources(ctx, namespace)
		if err != nil {
			return fmt.Errorf("export sources: %w", err)
		}
		for _, src := range sources {
			data, err := json.Marshal(src)
			if err != nil {
				return fmt.Errorf("marshal source %s: %w", src.ID, err)
			}
			if err := enc.Encode(record{Type: recordSource, Data: data}); err != nil {
				return fmt.Errorf("encode source %s: %w", src.ID, err)
			}
		}
	}

	return nil
}

// ExportFromSeeds exports nodes reachable from the given seed IDs, their
// edges, and associated sources.
func (e *Exporter) ExportFromSeeds(ctx context.Context, namespace string, seedIDs []uuid.UUID, maxDepth int, w io.Writer) error {
	if maxDepth <= 0 {
		maxDepth = 10
	}

	enc := json.NewEncoder(w)
	visited := make(map[uuid.UUID]bool)

	// BFS walk to collect all reachable nodes
	var allNodes []core.Node
	queue := make([]uuid.UUID, len(seedIDs))
	copy(queue, seedIDs)

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []uuid.UUID
		for _, id := range queue {
			if visited[id] {
				continue
			}
			visited[id] = true

			node, err := e.graph.GetNode(ctx, namespace, id)
			if err != nil {
				return fmt.Errorf("get node %s: %w", id, err)
			}
			if node == nil {
				continue
			}

			// Export all versions
			versions, err := e.graph.History(ctx, namespace, id)
			if err != nil {
				return fmt.Errorf("history %s: %w", id, err)
			}
			allNodes = append(allNodes, versions...)

			// Discover neighbors
			edges, err := e.graph.EdgesFrom(ctx, namespace, id, nil)
			if err != nil {
				return fmt.Errorf("edges from %s: %w", id, err)
			}
			for _, edge := range edges {
				if !visited[edge.Dst] {
					next = append(next, edge.Dst)
				}
			}
		}
		queue = next
	}

	// Write nodes
	for _, n := range allNodes {
		data, err := json.Marshal(n)
		if err != nil {
			return fmt.Errorf("marshal node: %w", err)
		}
		if err := enc.Encode(record{Type: recordNode, Data: data}); err != nil {
			return err
		}
	}

	// Write edges for visited nodes
	for id := range visited {
		edges, err := e.graph.EdgesFrom(ctx, namespace, id, nil)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			data, err := json.Marshal(edge)
			if err != nil {
				continue
			}
			if err := enc.Encode(record{Type: recordEdge, Data: data}); err != nil {
				return err
			}
		}
	}

	return nil
}

// Importer reads NDJSON and writes records into the graph and vector stores.
type Importer struct {
	graph store.GraphStore
	vecs  store.VectorIndex
}

// ImportReport summarizes records processed during snapshot import.
type ImportReport struct {
	Namespace          string `json:"namespace"`
	DryRun             bool   `json:"dry_run"`
	Lines              int    `json:"lines"`
	Nodes              int    `json:"nodes"`
	Edges              int    `json:"edges"`
	Sources            int    `json:"sources"`
	Vectors            int    `json:"vectors"`
	NamespaceOverrides int    `json:"namespace_overrides"`
}

// NewImporter creates a new Importer that writes to the given stores.
func NewImporter(graph store.GraphStore, vecs store.VectorIndex) *Importer {
	return &Importer{graph: graph, vecs: vecs}
}

// Import reads NDJSON from r and writes all records into the stores
// under the given namespace. If a record specifies a different namespace,
// the provided namespace overrides it.
func (im *Importer) Import(ctx context.Context, namespace string, r io.Reader) error {
	_, err := im.ImportWithReport(ctx, namespace, r)
	return err
}

// ImportWithReport imports an NDJSON snapshot and returns processed counts.
func (im *Importer) ImportWithReport(ctx context.Context, namespace string, r io.Reader) (ImportReport, error) {
	report := ImportReport{Namespace: namespace}
	scanner := bufio.NewScanner(r)
	// Allow large lines (up to 10MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		report.Lines++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return report, fmt.Errorf("line %d: unmarshal record: %w", lineNum, err)
		}

		switch rec.Type {
		case recordNode:
			var node core.Node
			if err := json.Unmarshal(rec.Data, &node); err != nil {
				return report, fmt.Errorf("line %d: unmarshal node: %w", lineNum, err)
			}
			if namespace != "" && node.Namespace != namespace {
				report.NamespaceOverrides++
				node.Namespace = namespace
			}
			if err := im.graph.UpsertNode(ctx, node); err != nil {
				return report, fmt.Errorf("line %d: upsert node %s: %w", lineNum, node.ID, err)
			}
			report.Nodes++
			// Index vector if present
			if len(node.Vector) > 0 {
				if reg, ok := im.vecs.(interface{ RegisterNode(core.Node) }); ok {
					reg.RegisterNode(node)
				}
				nID := node.ID
				if err := im.vecs.Index(ctx, core.VectorEntry{
					ID:        uuid.New(),
					Namespace: node.Namespace,
					NodeID:    &nID,
					Vector:    node.Vector,
					ModelID:   node.ModelID,
					CreatedAt: node.TxTime,
				}); err != nil {
					return report, fmt.Errorf("line %d: index vector for node %s: %w", lineNum, node.ID, err)
				}
				report.Vectors++
			}

		case recordEdge:
			var edge core.Edge
			if err := json.Unmarshal(rec.Data, &edge); err != nil {
				return report, fmt.Errorf("line %d: unmarshal edge: %w", lineNum, err)
			}
			if namespace != "" && edge.Namespace != namespace {
				report.NamespaceOverrides++
				edge.Namespace = namespace
			}
			if err := im.graph.UpsertEdge(ctx, edge); err != nil {
				return report, fmt.Errorf("line %d: upsert edge %s: %w", lineNum, edge.ID, err)
			}
			report.Edges++

		case recordSource:
			var src core.Source
			if err := json.Unmarshal(rec.Data, &src); err != nil {
				return report, fmt.Errorf("line %d: unmarshal source: %w", lineNum, err)
			}
			if namespace != "" && src.Namespace != namespace {
				report.NamespaceOverrides++
				src.Namespace = namespace
			}
			if err := im.graph.UpsertSource(ctx, src); err != nil {
				return report, fmt.Errorf("line %d: upsert source %s: %w", lineNum, src.ID, err)
			}
			report.Sources++

		default:
			return report, fmt.Errorf("line %d: unknown record type: %q", lineNum, rec.Type)
		}
	}

	if err := scanner.Err(); err != nil {
		return report, fmt.Errorf("read snapshot: %w", err)
	}

	return report, nil
}
