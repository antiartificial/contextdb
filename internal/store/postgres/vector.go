package postgres

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// VectorIndex implements store.VectorIndex backed by PostgreSQL.
// Vectors are stored as raw bytes and searched via brute-force cosine similarity
// in SQL. For large datasets, add a pgvector HNSW index on the vector column.
type VectorIndex struct {
	pool *pgxpool.Pool
}

// NewVectorIndex returns a VectorIndex backed by the given connection pool.
func NewVectorIndex(pool *pgxpool.Pool) *VectorIndex {
	return &VectorIndex{pool: pool}
}

func (v *VectorIndex) Index(ctx context.Context, entry core.VectorEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	vecBytes := float32sToBytes(entry.Vector)

	_, err := v.pool.Exec(ctx, `
		INSERT INTO vector_entries (id, namespace, node_id, vector, text, model_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			text = EXCLUDED.text,
			model_id = EXCLUDED.model_id
	`, entry.ID, entry.Namespace, entry.NodeID, vecBytes, entry.Text, entry.ModelID, entry.CreatedAt)
	return err
}

func (v *VectorIndex) Delete(ctx context.Context, ns string, id uuid.UUID) error {
	_, err := v.pool.Exec(ctx, "DELETE FROM vector_entries WHERE id = $1 AND namespace = $2", id, ns)
	return err
}

func (v *VectorIndex) Search(ctx context.Context, q store.VectorQuery) ([]core.ScoredNode, error) {
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	topK := q.TopK
	if topK <= 0 {
		topK = 20
	}

	// Fetch all vectors in namespace and compute cosine similarity in Go.
	// For production, use pgvector's <=> operator with a vector column.
	rows, err := v.pool.Query(ctx, `
		SELECT ve.id, ve.namespace, ve.node_id, ve.vector, ve.text, ve.model_id, ve.created_at,
		       n.id, n.namespace, n.labels, n.properties, n.model_id, n.valid_from, n.valid_until, n.tx_time, n.confidence, n.version
		FROM vector_entries ve
		LEFT JOIN LATERAL (
			SELECT * FROM nodes
			WHERE namespace = ve.namespace AND id = ve.node_id
			ORDER BY version DESC LIMIT 1
		) n ON TRUE
		WHERE ve.namespace = $1
	`, q.Namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		node       core.Node
		similarity float64
		hasNode    bool
	}

	labelSet := sliceToSet(q.Labels)
	var candidates []candidate

	for rows.Next() {
		var (
			veID, veNS, veText, veModel string
			veNodeID                     *uuid.UUID
			vecBytes                     []byte
			veCreatedAt                  time.Time
			nID                          *uuid.UUID
			nNS                          *string
			nLabels                      []string
			nProps                       []byte
			nModelID                     *string
			nValidFrom                   *time.Time
			nValidUntil                  *time.Time
			nTxTime                      *time.Time
			nConfidence                  *float64
			nVersion                     *uint64
		)

		err := rows.Scan(
			&veID, &veNS, &veNodeID, &vecBytes, &veText, &veModel, &veCreatedAt,
			&nID, &nNS, &nLabels, &nProps, &nModelID, &nValidFrom, &nValidUntil, &nTxTime, &nConfidence, &nVersion,
		)
		if err != nil {
			return nil, err
		}

		vec := bytesToFloat32s(vecBytes)
		sim := core.CosineSimilarity(q.Vector, vec)
		if sim <= 0 {
			continue
		}

		var node core.Node
		hasNode := nID != nil
		if hasNode {
			node.ID = *nID
			if nNS != nil {
				node.Namespace = *nNS
			}
			node.Labels = nLabels
			if len(nProps) > 0 {
				_ = json.Unmarshal(nProps, &node.Properties)
			}
			if node.Properties == nil {
				node.Properties = make(map[string]any)
			}
			if nModelID != nil {
				node.ModelID = *nModelID
			}
			if nValidFrom != nil {
				node.ValidFrom = *nValidFrom
			}
			node.ValidUntil = nValidUntil
			if nTxTime != nil {
				node.TxTime = *nTxTime
			}
			if nConfidence != nil {
				node.Confidence = *nConfidence
			}
			if nVersion != nil {
				node.Version = *nVersion
			}

			if !node.IsValidAt(asOf) {
				continue
			}
			if len(labelSet) > 0 {
				allMatch := true
				for lbl := range labelSet {
					if !node.HasLabel(lbl) {
						allMatch = false
						break
					}
				}
				if !allMatch {
					continue
				}
			}
		} else {
			node = core.Node{
				Namespace:  veNS,
				Properties: map[string]any{"text": veText},
			}
		}

		candidates = append(candidates, candidate{node: node, similarity: sim, hasNode: hasNode})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})
	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	out := make([]core.ScoredNode, len(candidates))
	for i, c := range candidates {
		out[i] = core.ScoredNode{
			Node:            c.node,
			Score:           c.similarity,
			SimilarityScore: c.similarity,
			RetrievalSource: "vector",
		}
	}
	return out, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func float32sToBytes(fs []float32) []byte {
	buf := make([]byte, len(fs)*4)
	for i, f := range fs {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func bytesToFloat32s(b []byte) []float32 {
	n := len(b) / 4
	fs := make([]float32, n)
	for i := 0; i < n; i++ {
		fs[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return fs
}

func sliceToSet(ss []string) map[string]bool {
	if len(ss) == 0 {
		return nil
	}
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
