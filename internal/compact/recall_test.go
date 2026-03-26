package compact

import (
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestRecallConfig_Defaults(t *testing.T) {
	is := is.New(t)

	cfg := RecallConfig{}
	cfg = cfg.withDefaults()

	is.Equal(cfg.Interval, 1*time.Hour)
	is.Equal(cfg.QueriesPerCycle, 10)
	is.Equal(cfg.BoostAmount, 0.05)
	is.Equal(cfg.DecayAmount, 0.01)
}

func TestRecallConfig_CustomValues(t *testing.T) {
	is := is.New(t)

	cfg := RecallConfig{
		Interval:        30 * time.Minute,
		QueriesPerCycle: 20,
		BoostAmount:     0.1,
		DecayAmount:     0.02,
		Namespaces:      []string{"ns1"},
	}
	cfg = cfg.withDefaults()

	is.Equal(cfg.Interval, 30*time.Minute)
	is.Equal(cfg.QueriesPerCycle, 20)
	is.Equal(cfg.BoostAmount, 0.1)
	is.Equal(cfg.DecayAmount, 0.02)
	is.Equal(len(cfg.Namespaces), 1)
}

func TestNewRecallWorker(t *testing.T) {
	is := is.New(t)

	w := NewRecallWorker(nil, nil, RecallConfig{}, nil)
	is.True(w != nil)
	is.Equal(w.config.Interval, 1*time.Hour)
	is.Equal(w.config.QueriesPerCycle, 10)
	is.Equal(w.config.BoostAmount, 0.05)
	is.Equal(w.config.DecayAmount, 0.01)
}

func TestRandomVector(t *testing.T) {
	is := is.New(t)

	vec := randomVector(8)
	is.Equal(len(vec), 8)

	// Values should be in [-1, 1]
	for _, v := range vec {
		is.True(v >= -1.0)
		is.True(v <= 1.0)
	}

	// Two random vectors should (almost certainly) be different
	vec2 := randomVector(8)
	different := false
	for i := range vec {
		if vec[i] != vec2[i] {
			different = true
			break
		}
	}
	is.True(different)
}
