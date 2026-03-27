package federation

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hashicorp/memberlist"
)

// NodeMeta is the metadata exchanged via memberlist gossip.
type NodeMeta struct {
	GRPCAddr   string           `json:"g"`
	PeerID     string           `json:"p"`
	Namespaces map[string]int64 `json:"n"` // ns → TxTime.UnixNano()
}

// federationDelegate implements memberlist.Delegate.
type federationDelegate struct {
	federation *Federation
	logger     *slog.Logger
}

func (d *federationDelegate) NodeMeta(limit int) []byte {
	meta := NodeMeta{
		GRPCAddr:   d.federation.config.AdvertiseAddr,
		PeerID:     d.federation.peerID,
		Namespaces: d.federation.getWatermarks(),
	}
	data, err := json.Marshal(meta)
	if err != nil || len(data) > limit {
		return nil
	}
	return data
}

func (d *federationDelegate) NotifyMsg(_ []byte) {
	// Gossip messages — not used for cursor exchange (we use metadata).
}

func (d *federationDelegate) GetBroadcasts(_, _ int) [][]byte {
	return nil
}

func (d *federationDelegate) LocalState(_ bool) []byte {
	return nil
}

func (d *federationDelegate) MergeRemoteState(_ []byte, _ bool) {
	// Could be used for bulk state transfer on join.
}

// federationEvents implements memberlist.EventDelegate for join/leave.
type federationEvents struct {
	federation *Federation
	logger     *slog.Logger
}

func (e *federationEvents) NotifyJoin(node *memberlist.Node) {
	meta := parseNodeMeta(node.Meta)
	if meta == nil || meta.PeerID == e.federation.peerID {
		return
	}
	wm := make(map[string]time.Time, len(meta.Namespaces))
	for ns, ts := range meta.Namespaces {
		wm[ns] = time.Unix(0, ts)
	}
	e.federation.peers.Update(Peer{
		ID:         meta.PeerID,
		GRPCAddr:   meta.GRPCAddr,
		Watermarks: wm,
		LastSeen:   time.Now(),
		Alive:      true,
	})
	e.logger.Info("peer joined", "peer", meta.PeerID, "addr", meta.GRPCAddr)
}

func (e *federationEvents) NotifyLeave(node *memberlist.Node) {
	meta := parseNodeMeta(node.Meta)
	if meta == nil {
		return
	}
	e.federation.peers.SetAlive(meta.PeerID, false)
	e.logger.Info("peer left", "peer", meta.PeerID)
}

func (e *federationEvents) NotifyUpdate(node *memberlist.Node) {
	meta := parseNodeMeta(node.Meta)
	if meta == nil || meta.PeerID == e.federation.peerID {
		return
	}
	wm := make(map[string]time.Time, len(meta.Namespaces))
	for ns, ts := range meta.Namespaces {
		wm[ns] = time.Unix(0, ts)
	}
	e.federation.peers.Update(Peer{
		ID:         meta.PeerID,
		GRPCAddr:   meta.GRPCAddr,
		Watermarks: wm,
		LastSeen:   time.Now(),
		Alive:      true,
	})
}

// parseNodeMeta decodes a raw metadata byte slice into NodeMeta.
// Returns nil on any error or empty input.
func parseNodeMeta(data []byte) *NodeMeta {
	if len(data) == 0 {
		return nil
	}
	var meta NodeMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}
