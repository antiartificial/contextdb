package federation

import (
	"testing"
	"time"
)

// ─── PeerRegistry tests ──────────────────────────────────────────────────────

func TestPeerRegistry_UpdateAndGet(t *testing.T) {
	r := NewPeerRegistry()

	p := Peer{
		ID:       "peer-1",
		GRPCAddr: "10.0.0.1:7700",
		Alive:    true,
	}
	r.Update(p)

	got, ok := r.Get("peer-1")
	if !ok {
		t.Fatal("expected peer-1 to be found")
	}
	if got.GRPCAddr != p.GRPCAddr {
		t.Errorf("GRPCAddr: got %q, want %q", got.GRPCAddr, p.GRPCAddr)
	}
	if !got.Alive {
		t.Error("expected peer to be alive")
	}
}

func TestPeerRegistry_GetMissing(t *testing.T) {
	r := NewPeerRegistry()
	_, ok := r.Get("nope")
	if ok {
		t.Error("expected ok=false for missing peer")
	}
}

func TestPeerRegistry_Remove(t *testing.T) {
	r := NewPeerRegistry()
	r.Update(Peer{ID: "peer-2", Alive: true})
	r.Remove("peer-2")

	_, ok := r.Get("peer-2")
	if ok {
		t.Error("expected peer-2 to be removed")
	}
}

func TestPeerRegistry_AlivePeers(t *testing.T) {
	r := NewPeerRegistry()
	r.Update(Peer{ID: "alive-1", Alive: true})
	r.Update(Peer{ID: "alive-2", Alive: true})
	r.Update(Peer{ID: "dead-1", Alive: false})

	alive := r.AlivePeers()
	if len(alive) != 2 {
		t.Errorf("expected 2 alive peers, got %d", len(alive))
	}
	for _, p := range alive {
		if !p.Alive {
			t.Errorf("non-alive peer in AlivePeers result: %s", p.ID)
		}
	}
}

func TestPeerRegistry_AlivePeers_EmptyWhenNone(t *testing.T) {
	r := NewPeerRegistry()
	if peers := r.AlivePeers(); len(peers) != 0 {
		t.Errorf("expected empty slice, got %d peers", len(peers))
	}
}

func TestPeerRegistry_SetAlive_MarksDead(t *testing.T) {
	r := NewPeerRegistry()
	r.Update(Peer{ID: "peer-3", Alive: true})

	before := time.Now()
	r.SetAlive("peer-3", false)

	got, ok := r.Get("peer-3")
	if !ok {
		t.Fatal("peer-3 should still exist after SetAlive")
	}
	if got.Alive {
		t.Error("expected Alive=false after SetAlive(false)")
	}
	if got.LastSeen.Before(before) {
		t.Error("expected LastSeen to be updated")
	}
}

func TestPeerRegistry_SetAlive_NoopForMissing(t *testing.T) {
	r := NewPeerRegistry()
	// Must not panic for unknown peer.
	r.SetAlive("ghost", true)
}

func TestPeerRegistry_UpdateOverwrites(t *testing.T) {
	r := NewPeerRegistry()
	r.Update(Peer{ID: "peer-4", GRPCAddr: "old:7700", Alive: false})
	r.Update(Peer{ID: "peer-4", GRPCAddr: "new:7700", Alive: true})

	got, ok := r.Get("peer-4")
	if !ok {
		t.Fatal("peer-4 missing")
	}
	if got.GRPCAddr != "new:7700" {
		t.Errorf("GRPCAddr not overwritten: %q", got.GRPCAddr)
	}
	if !got.Alive {
		t.Error("Alive not overwritten to true")
	}
}

// ─── Config.withDefaults tests ───────────────────────────────────────────────

func TestConfig_WithDefaults_Filled(t *testing.T) {
	cfg := Config{}.withDefaults()

	if cfg.BindAddr != ":7710" {
		t.Errorf("BindAddr: got %q, want \":7710\"", cfg.BindAddr)
	}
	if cfg.PeerID == "" {
		t.Error("expected PeerID to be auto-generated")
	}
	if cfg.PullInterval != 5*time.Second {
		t.Errorf("PullInterval: got %v, want 5s", cfg.PullInterval)
	}
	if cfg.AntiEntropyInterval != time.Hour {
		t.Errorf("AntiEntropyInterval: got %v, want 1h", cfg.AntiEntropyInterval)
	}
	if cfg.MaxBatchSize != 1000 {
		t.Errorf("MaxBatchSize: got %d, want 1000", cfg.MaxBatchSize)
	}
}

func TestConfig_WithDefaults_PreservesExplicit(t *testing.T) {
	cfg := Config{
		BindAddr:            ":9999",
		PeerID:              "my-stable-id",
		PullInterval:        30 * time.Second,
		AntiEntropyInterval: 10 * time.Minute,
		MaxBatchSize:        500,
	}.withDefaults()

	if cfg.BindAddr != ":9999" {
		t.Errorf("BindAddr should not be overwritten, got %q", cfg.BindAddr)
	}
	if cfg.PeerID != "my-stable-id" {
		t.Errorf("PeerID should not be overwritten, got %q", cfg.PeerID)
	}
	if cfg.PullInterval != 30*time.Second {
		t.Errorf("PullInterval should not be overwritten, got %v", cfg.PullInterval)
	}
	if cfg.AntiEntropyInterval != 10*time.Minute {
		t.Errorf("AntiEntropyInterval should not be overwritten, got %v", cfg.AntiEntropyInterval)
	}
	if cfg.MaxBatchSize != 500 {
		t.Errorf("MaxBatchSize should not be overwritten, got %d", cfg.MaxBatchSize)
	}
}
