package federation

import (
	"encoding/json"
	"testing"
)

// ─── parseNodeMeta tests ──────────────────────────────────────────────────────

func TestParseNodeMeta_Valid(t *testing.T) {
	meta := NodeMeta{
		GRPCAddr:   "10.0.0.1:7700",
		PeerID:     "peer-abc",
		Namespaces: map[string]int64{"ns1": 1_700_000_000_000_000_000},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := parseNodeMeta(data)
	if got == nil {
		t.Fatal("expected non-nil NodeMeta")
	}
	if got.PeerID != meta.PeerID {
		t.Errorf("PeerID: got %q, want %q", got.PeerID, meta.PeerID)
	}
	if got.GRPCAddr != meta.GRPCAddr {
		t.Errorf("GRPCAddr: got %q, want %q", got.GRPCAddr, meta.GRPCAddr)
	}
	if ts, ok := got.Namespaces["ns1"]; !ok || ts != 1_700_000_000_000_000_000 {
		t.Errorf("Namespaces[ns1]: got %d, want 1700000000000000000", ts)
	}
}

func TestParseNodeMeta_EmptyInput(t *testing.T) {
	if got := parseNodeMeta(nil); got != nil {
		t.Error("expected nil for nil input")
	}
	if got := parseNodeMeta([]byte{}); got != nil {
		t.Error("expected nil for empty input")
	}
}

func TestParseNodeMeta_InvalidJSON(t *testing.T) {
	// These inputs cannot be unmarshalled into a NodeMeta struct at all.
	hardErrors := [][]byte{
		[]byte("not json"),
		[]byte("{broken"),
		[]byte("42"),
	}
	for _, c := range hardErrors {
		if got := parseNodeMeta(c); got != nil {
			t.Errorf("expected nil for input %q, got %+v", c, got)
		}
	}

	// JSON "null" is valid JSON and unmarshals to a zero-value struct; that is
	// acceptable behaviour — callers distinguish meaningful metadata by checking
	// PeerID rather than expecting a nil return for null.
}

func TestParseNodeMeta_EmptyNamespaces(t *testing.T) {
	meta := NodeMeta{
		GRPCAddr:   "host:7700",
		PeerID:     "peer-x",
		Namespaces: nil,
	}
	data, _ := json.Marshal(meta)
	got := parseNodeMeta(data)
	if got == nil {
		t.Fatal("expected non-nil NodeMeta")
	}
	if len(got.Namespaces) != 0 {
		t.Errorf("expected empty namespaces, got %v", got.Namespaces)
	}
}

// ─── NodeMeta serialization size test ────────────────────────────────────────

// memberlist enforces a 512-byte limit on node metadata.
const memberlistMetaLimit = 512

func TestNodeMeta_SizeUnderLimit_NoNamespaces(t *testing.T) {
	meta := NodeMeta{
		GRPCAddr:   "192.168.100.200:7700",
		PeerID:     "550e8400-e29b-41d4-a716-446655440000", // full UUID
		Namespaces: map[string]int64{},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) >= memberlistMetaLimit {
		t.Errorf("metadata too large: %d bytes (limit %d)", len(data), memberlistMetaLimit)
	}
}

func TestNodeMeta_SizeUnderLimit_TenNamespaces(t *testing.T) {
	// Simulate ten namespaces each with a realistic short name (≤8 chars) and a
	// nanosecond timestamp. NodeMeta uses compact JSON field names ("g","p","n")
	// so typical production payloads stay well under 512 bytes.
	namespaces := make(map[string]int64, 10)
	for i := 0; i < 10; i++ {
		ns := "ns" + string(rune('0'+i)) // e.g. "ns0" … "ns9"
		namespaces[ns] = 1_700_000_000_000_000_000
	}
	meta := NodeMeta{
		GRPCAddr:   "192.168.100.200:7700",
		PeerID:     "550e8400-e29b-41d4-a716-446655440000",
		Namespaces: namespaces,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) >= memberlistMetaLimit {
		t.Errorf("metadata too large with 10 namespaces: %d bytes (limit %d)", len(data), memberlistMetaLimit)
	}
}

func TestNodeMeta_RoundTrip(t *testing.T) {
	original := NodeMeta{
		GRPCAddr:   "node.example.com:7700",
		PeerID:     "test-peer-roundtrip",
		Namespaces: map[string]int64{"alpha": 100, "beta": 200, "gamma": 300},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := parseNodeMeta(data)
	if got == nil {
		t.Fatal("expected non-nil on round-trip")
	}
	if got.GRPCAddr != original.GRPCAddr {
		t.Errorf("GRPCAddr mismatch: %q vs %q", got.GRPCAddr, original.GRPCAddr)
	}
	if got.PeerID != original.PeerID {
		t.Errorf("PeerID mismatch: %q vs %q", got.PeerID, original.PeerID)
	}
	for ns, want := range original.Namespaces {
		if got.Namespaces[ns] != want {
			t.Errorf("Namespaces[%q]: got %d, want %d", ns, got.Namespaces[ns], want)
		}
	}
}
