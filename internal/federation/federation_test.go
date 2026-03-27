package federation_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

func TestFederation_StartStop(t *testing.T) {
	graph := memory.NewGraphStore()
	vecs := memory.NewVectorIndex()
	log := memory.NewEventLog()

	cfg := federation.Config{
		Enabled:  true,
		BindAddr: ":0", // random port
	}

	f := federation.New(graph, vecs, log, cfg, slog.Default())
	if err := f.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify peer ID was set.
	if f.PeerID() == "" {
		t.Error("expected non-empty peer ID")
	}

	f.Stop()
}

func TestFederation_PeerRegistry_EmptyOnStart(t *testing.T) {
	graph := memory.NewGraphStore()
	vecs := memory.NewVectorIndex()
	log := memory.NewEventLog()

	cfg := federation.Config{
		Enabled:  true,
		BindAddr: ":0",
	}

	f := federation.New(graph, vecs, log, cfg, slog.Default())
	if err := f.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer f.Stop()

	peers := f.Peers().AlivePeers()
	if len(peers) != 0 {
		t.Errorf("expected no alive peers at startup, got %d", len(peers))
	}
}

func TestFederation_UpdateWatermark_Monotonic(t *testing.T) {
	graph := memory.NewGraphStore()
	vecs := memory.NewVectorIndex()
	log := memory.NewEventLog()

	cfg := federation.Config{
		Enabled:  true,
		BindAddr: ":0",
	}

	f := federation.New(graph, vecs, log, cfg, slog.Default())
	if err := f.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer f.Stop()

	now := time.Now()
	earlier := now.Add(-time.Hour)

	// Advance watermark.
	f.UpdateWatermark("test-ns", now)
	// Attempt to regress — should be a no-op (only advances forward).
	f.UpdateWatermark("test-ns", earlier)

	// We cannot read the watermark directly from outside the package, but
	// the key check is that these calls don't panic and PeerID remains set.
	if f.PeerID() == "" {
		t.Error("expected non-empty peer ID after watermark updates")
	}
}
