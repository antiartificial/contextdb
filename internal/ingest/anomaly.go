package ingest

import (
	"time"
)

// AnomalySignal represents an anomalous pattern detected during ingestion.
type AnomalySignal struct {
	Namespace  string
	Type       string  // "rate_spike", "confidence_drop", "source_burst"
	Message    string
	Severity   float64 // 0-1
	DetectedAt time.Time
}

// WriteRateDetector monitors ingestion write rates per source for anomalies.
// It complements the source-level AnomalyDetector in consensus.go, which
// operates on credibility history snapshots. WriteRateDetector operates on
// real-time write counts within a sliding one-second window.
type WriteRateDetector struct {
	// Track write rates per source per second
	sourceRates map[string]int64
	lastReset   time.Time
	signals     []AnomalySignal
}

// NewWriteRateDetector creates a write-rate anomaly detector.
func NewWriteRateDetector() *WriteRateDetector {
	return &WriteRateDetector{
		sourceRates: make(map[string]int64),
		lastReset:   time.Now(),
	}
}

// RecordWrite records a write from a source and checks for anomalies.
// Returns a non-nil AnomalySignal when a source_burst pattern is detected.
func (d *WriteRateDetector) RecordWrite(ns, sourceID string) *AnomalySignal {
	now := time.Now()
	if now.Sub(d.lastReset) > time.Second {
		d.sourceRates = make(map[string]int64)
		d.lastReset = now
	}

	key := ns + ":" + sourceID
	d.sourceRates[key]++
	rate := d.sourceRates[key]

	// Flag if a source writes > 50 times per second
	if rate > 50 {
		sig := &AnomalySignal{
			Namespace:  ns,
			Type:       "source_burst",
			Message:    "source " + sourceID + " writing at elevated rate",
			Severity:   min(float64(rate)/100.0, 1.0),
			DetectedAt: now,
		}
		d.signals = append(d.signals, *sig)
		return sig
	}
	return nil
}

// Signals returns all recorded anomaly signals.
func (d *WriteRateDetector) Signals() []AnomalySignal {
	return d.signals
}
