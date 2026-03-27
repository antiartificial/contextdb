package client

import (
	"fmt"

	"github.com/google/uuid"
)

// topoSort returns indices of reqs in topological order based on
// DependsOn fields. A request i depends on request j when i.DependsOn
// contains a UUID that matches j's Properties["node_id"]. Dependencies
// referencing IDs outside the batch are ignored.
//
// Returns an error if the dependency graph contains a cycle.
func topoSort(reqs []WriteRequest) ([]int, error) {
	n := len(reqs)

	// Map pre-assigned node_id → request index.
	idToIdx := make(map[uuid.UUID]int, n)
	for i, req := range reqs {
		if req.Properties == nil {
			continue
		}
		raw, ok := req.Properties["node_id"]
		if !ok {
			continue
		}
		if uid, ok := raw.(uuid.UUID); ok {
			idToIdx[uid] = i
		}
	}

	// inDegree[i] = number of in-batch dependencies for request i.
	// dependents[j] = indices of requests that depend on j.
	inDegree := make([]int, n)
	dependents := make([][]int, n)

	for i, req := range reqs {
		for _, dep := range req.DependsOn {
			j, ok := idToIdx[dep]
			if !ok {
				continue // dependency outside this batch; skip
			}
			inDegree[i]++
			dependents[j] = append(dependents[j], i)
		}
	}

	// Kahn's algorithm.
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	order := make([]int, 0, n)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)

		for _, dep := range dependents[curr] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != n {
		return nil, fmt.Errorf("dependency cycle detected in batch of %d requests", n)
	}
	return order, nil
}
