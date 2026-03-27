package observe

import (
	"context"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// ConfidenceTrajectory is the time-series of confidence for a single node.
type ConfidenceTrajectory struct {
	NodeID uuid.UUID
	Points []ConfidencePoint
}

// GetConfidenceTrajectory returns the confidence history for a node
// by walking its version history.
func GetConfidenceTrajectory(ctx context.Context, graph store.GraphStore, ns string, nodeID uuid.UUID) (*ConfidenceTrajectory, error) {
	versions, err := graph.History(ctx, ns, nodeID)
	if err != nil {
		return nil, err
	}

	traj := &ConfidenceTrajectory{NodeID: nodeID}
	for _, v := range versions {
		traj.Points = append(traj.Points, ConfidencePoint{
			Time:       v.TxTime,
			Confidence: v.Confidence,
			Version:    v.Version,
		})
	}
	return traj, nil
}
