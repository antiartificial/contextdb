package federation

import (
	"time"

	"github.com/google/uuid"
)

// Config holds all federation settings. Zero value = disabled.
type Config struct {
	Enabled             bool
	PeerID              string        // stable instance ID (auto-generated if empty)
	BindAddr            string        // memberlist bind (default ":7710")
	AdvertiseAddr       string        // public address (default: auto-detect)
	SeedPeers           []string      // initial peer addresses for joining
	Namespaces          []string      // namespaces to federate; nil = all
	PullInterval        time.Duration // how often to pull from peers (default 5s)
	AntiEntropyInterval time.Duration // full reconciliation (default 1h)
	MaxBatchSize        int           // max events per pull (default 1000)
}

func (c Config) withDefaults() Config {
	if c.BindAddr == "" {
		c.BindAddr = ":7710"
	}
	if c.PeerID == "" {
		c.PeerID = uuid.New().String()
	}
	if c.PullInterval == 0 {
		c.PullInterval = 5 * time.Second
	}
	if c.AntiEntropyInterval == 0 {
		c.AntiEntropyInterval = time.Hour
	}
	if c.MaxBatchSize == 0 {
		c.MaxBatchSize = 1000
	}
	return c
}
