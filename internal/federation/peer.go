package federation

import (
	"sync"
	"time"
)

// Peer represents a known federation peer.
type Peer struct {
	ID         string
	GRPCAddr   string
	Watermarks map[string]time.Time // ns → latest TxTime
	LastSeen   time.Time
	Alive      bool
}

// PeerRegistry tracks known peers (thread-safe).
type PeerRegistry struct {
	mu    sync.RWMutex
	peers map[string]*Peer // peerID → Peer
}

// NewPeerRegistry returns an empty PeerRegistry.
func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{peers: make(map[string]*Peer)}
}

// Update inserts or replaces the peer entry.
func (r *PeerRegistry) Update(p Peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[p.ID] = &p
}

// Get returns the peer for the given ID, and whether it was found.
func (r *PeerRegistry) Get(id string) (*Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[id]
	return p, ok
}

// Remove deletes the peer entry entirely.
func (r *PeerRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, id)
}

// AlivePeers returns all peers currently marked alive.
func (r *PeerRegistry) AlivePeers() []*Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Peer
	for _, p := range r.peers {
		if p.Alive {
			result = append(result, p)
		}
	}
	return result
}

// SetAlive marks the peer alive/dead and refreshes LastSeen.
func (r *PeerRegistry) SetAlive(id string, alive bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		p.Alive = alive
		p.LastSeen = time.Now()
	}
}
