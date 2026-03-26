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
	is.Equal(cfg.MaxReviewsPerCycle, 20)
}

func TestRecallConfig_CustomValues(t *testing.T) {
	is := is.New(t)

	cfg := RecallConfig{
		Interval:           30 * time.Minute,
		MaxReviewsPerCycle: 50,
		Namespaces:         []string{"ns1"},
	}
	cfg = cfg.withDefaults()

	is.Equal(cfg.Interval, 30*time.Minute)
	is.Equal(cfg.MaxReviewsPerCycle, 50)
	is.Equal(len(cfg.Namespaces), 1)
}

func TestNewRecallWorker(t *testing.T) {
	is := is.New(t)

	w := NewRecallWorker(nil, nil, RecallConfig{}, nil)
	is.True(w != nil)
	is.Equal(w.config.Interval, 1*time.Hour)
	is.Equal(w.config.MaxReviewsPerCycle, 20)
}


