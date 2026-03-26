package compact

import (
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
)

func TestConsolidationConfig_Defaults(t *testing.T) {
	is := is.New(t)

	cfg := ConsolidationConfig{}
	cfg = cfg.withDefaults()

	is.Equal(cfg.AgeThreshold, 24*time.Hour)
	is.Equal(cfg.FrequencyThreshold, 3)
	is.Equal(cfg.Interval, 30*time.Minute)
}

func TestConsolidationConfig_CustomValues(t *testing.T) {
	is := is.New(t)

	cfg := ConsolidationConfig{
		AgeThreshold:       12 * time.Hour,
		FrequencyThreshold: 5,
		Interval:           10 * time.Minute,
		Namespaces:         []string{"ns1", "ns2"},
	}
	cfg = cfg.withDefaults()

	is.Equal(cfg.AgeThreshold, 12*time.Hour)
	is.Equal(cfg.FrequencyThreshold, 5)
	is.Equal(cfg.Interval, 10*time.Minute)
	is.Equal(len(cfg.Namespaces), 2)
}

func TestNewConsolidator(t *testing.T) {
	is := is.New(t)

	c := NewConsolidator(nil, nil, nil, nil, ConsolidationConfig{}, nil)
	is.True(c != nil)
	is.Equal(c.config.AgeThreshold, 24*time.Hour)
	is.Equal(c.config.FrequencyThreshold, 3)
	is.Equal(c.config.Interval, 30*time.Minute)
}

func TestIsEpisodicMemory(t *testing.T) {
	is := is.New(t)

	// Test with label
	node := &core.Node{
		Labels: []string{"episodic"},
	}
	is.True(isEpisodicMemory(node))

	// Test with Episode label
	node2 := &core.Node{
		Labels: []string{"Episode"},
	}
	is.True(isEpisodicMemory(node2))

	// Test with memory_type property
	node3 := &core.Node{
		Properties: map[string]any{"memory_type": "episodic"},
	}
	is.True(isEpisodicMemory(node3))

	// Test non-episodic
	node4 := &core.Node{
		Labels: []string{"semantic"},
	}
	is.True(!isEpisodicMemory(node4))
}
