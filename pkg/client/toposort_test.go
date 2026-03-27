package client

import (
	"testing"

	"github.com/google/uuid"
)

func TestTopoSort_NoDependencies(t *testing.T) {
	reqs := []WriteRequest{
		{Content: "a"},
		{Content: "b"},
		{Content: "c"},
	}
	order, err := topoSort(reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 indices, got %d", len(order))
	}
	// With no dependencies all should appear; order is stable (0,1,2).
	for i, idx := range order {
		if idx != i {
			t.Errorf("order[%d] = %d, want %d", i, idx, i)
		}
	}
}

func TestTopoSort_LinearChain(t *testing.T) {
	idA := uuid.New()
	idB := uuid.New()
	idC := uuid.New()

	// C depends on B, B depends on A → expected order: A, B, C
	reqs := []WriteRequest{
		{ // index 0: A
			Content:    "a",
			Properties: map[string]any{"node_id": idA},
		},
		{ // index 1: B — depends on A
			Content:    "b",
			Properties: map[string]any{"node_id": idB},
			DependsOn:  []uuid.UUID{idA},
		},
		{ // index 2: C — depends on B
			Content:   "c",
			Properties: map[string]any{"node_id": idC},
			DependsOn: []uuid.UUID{idB},
		},
	}
	order, err := topoSort(reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A (0) must come before B (1), B before C (2).
	pos := make(map[int]int, len(order))
	for i, idx := range order {
		pos[idx] = i
	}
	if pos[0] >= pos[1] {
		t.Errorf("A (pos %d) should come before B (pos %d)", pos[0], pos[1])
	}
	if pos[1] >= pos[2] {
		t.Errorf("B (pos %d) should come before C (pos %d)", pos[1], pos[2])
	}
}

func TestTopoSort_CycleDetected(t *testing.T) {
	idA := uuid.New()
	idB := uuid.New()

	// A depends on B, B depends on A → cycle
	reqs := []WriteRequest{
		{
			Content:    "a",
			Properties: map[string]any{"node_id": idA},
			DependsOn:  []uuid.UUID{idB},
		},
		{
			Content:    "b",
			Properties: map[string]any{"node_id": idB},
			DependsOn:  []uuid.UUID{idA},
		},
	}
	_, err := topoSort(reqs)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestTopoSort_ExternalDepsIgnored(t *testing.T) {
	externalID := uuid.New()
	idA := uuid.New()

	// Request 1 depends on an ID not in the batch — should be ignored.
	reqs := []WriteRequest{
		{
			Content:    "a",
			Properties: map[string]any{"node_id": idA},
			DependsOn:  []uuid.UUID{externalID},
		},
		{
			Content: "b",
		},
	}
	order, err := topoSort(reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 indices, got %d", len(order))
	}
}

func TestTopoSort_DiamondDependency(t *testing.T) {
	idA := uuid.New()
	idB := uuid.New()
	idC := uuid.New()

	// D depends on B and C; B and C both depend on A.
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	reqs := []WriteRequest{
		{ // 0: A
			Content:    "a",
			Properties: map[string]any{"node_id": idA},
		},
		{ // 1: B
			Content:    "b",
			Properties: map[string]any{"node_id": idB},
			DependsOn:  []uuid.UUID{idA},
		},
		{ // 2: C
			Content:    "c",
			Properties: map[string]any{"node_id": idC},
			DependsOn:  []uuid.UUID{idA},
		},
		{ // 3: D
			Content:   "d",
			DependsOn: []uuid.UUID{idB, idC},
		},
	}

	order, err := topoSort(reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos := make(map[int]int, len(order))
	for i, idx := range order {
		pos[idx] = i
	}

	// A before B and C; B and C before D.
	if pos[0] >= pos[1] {
		t.Errorf("A (pos %d) should come before B (pos %d)", pos[0], pos[1])
	}
	if pos[0] >= pos[2] {
		t.Errorf("A (pos %d) should come before C (pos %d)", pos[0], pos[2])
	}
	if pos[1] >= pos[3] {
		t.Errorf("B (pos %d) should come before D (pos %d)", pos[1], pos[3])
	}
	if pos[2] >= pos[3] {
		t.Errorf("C (pos %d) should come before D (pos %d)", pos[2], pos[3])
	}
}
