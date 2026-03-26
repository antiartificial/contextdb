package core

import (
	"math"
	"time"
)

// SM2Data holds the SuperMemo-2 spaced repetition state for a memory node.
// This implements the classic SM-2 algorithm for active recall scheduling.
type SM2Data struct {
	// EasinessFactor (EF) determines how quickly intervals grow.
	// Range: [1.3, 2.5]. Default: 2.5.
	// Higher EF = easier item = longer intervals.
	EasinessFactor float64

	// Interval is the number of days until the next review.
	// First interval = 1 day, second = 6 days, then multiplies by EF.
	IntervalDays int

	// RepetitionCount is the number of successful consecutive reviews.
	// Resets to 0 on failure (quality < 3).
	RepetitionCount int

	// NextReviewDate is when this memory should next be reviewed.
	NextReviewDate time.Time

	// LastQuality is the most recent response quality (0-5).
	// 0 = complete blackout, 5 = perfect response.
	LastQuality int
}

// DefaultSM2Data returns initialized SM-2 data with EF=2.5, no prior repetitions.
func DefaultSM2Data() SM2Data {
	return SM2Data{
		EasinessFactor: 2.5,
		IntervalDays:   0,
		RepetitionCount: 0,
		NextReviewDate: time.Time{}, // zero = not scheduled yet
		LastQuality:    0,
	}
}

// IsDue reports whether this memory is due for review as of the given time.
func (s SM2Data) IsDue(asOf time.Time) bool {
	// If never scheduled, it's due immediately
	if s.NextReviewDate.IsZero() {
		return true
	}
	return !asOf.Before(s.NextReviewDate)
}

// Update applies a quality rating (0-5) and updates the SM-2 state.
//
// Quality ratings:
//   - 5: perfect response
//   - 4: correct response after hesitation
//   - 3: correct response with serious difficulty
//   - 2: incorrect response, seemed easy to recall
//   - 1: incorrect response, remembered the answer
//   - 0: complete blackout
//
// Returns the new SM2Data state.
func (s SM2Data) Update(quality int) SM2Data {
	// Clamp quality to [0, 5]
	if quality < 0 {
		quality = 0
	}
	if quality > 5 {
		quality = 5
	}

	s.LastQuality = quality

	// If quality < 3, treat as failure: reset repetition count
	if quality < 3 {
		s.RepetitionCount = 0
		s.IntervalDays = 1
		// Update easiness factor but don't let it drop below 1.3
		s.EasinessFactor = updateEF(s.EasinessFactor, quality)
		s.NextReviewDate = time.Now().AddDate(0, 0, s.IntervalDays)
		return s
	}

	// Successful recall
	s.RepetitionCount++
	s.EasinessFactor = updateEF(s.EasinessFactor, quality)

	// Calculate next interval
	if s.RepetitionCount == 1 {
		s.IntervalDays = 1
	} else if s.RepetitionCount == 2 {
		s.IntervalDays = 6
	} else {
		s.IntervalDays = int(math.Round(float64(s.IntervalDays) * s.EasinessFactor))
	}

	s.NextReviewDate = time.Now().AddDate(0, 0, s.IntervalDays)
	return s
}

// updateEF updates the easiness factor based on quality response.
// Formula from SuperMemo-2:
// EF' = EF + (0.1 - (5-q) * (0.08 + (5-q) * 0.02))
// where q is the quality (0-5).
// EF is clamped to minimum 1.3 (below which items become too difficult).
func updateEF(currentEF float64, quality int) float64 {
	q := float64(quality)
	delta := 0.1 - (5-q)*(0.08+(5-q)*0.02)
	newEF := currentEF + delta
	if newEF < 1.3 {
		return 1.3
	}
	return newEF
}

// PriorityScore returns a score [0, 1] indicating how urgently this
// memory needs review. Higher = more urgent.
// Factors in: overdue status, easiness factor decay, repetition history.
func (s SM2Data) PriorityScore(now time.Time) float64 {
	if !s.IsDue(now) {
		return 0
	}

	// Base priority from how overdue
	overdueDays := now.Sub(s.NextReviewDate).Hours() / 24
	if overdueDays < 0 {
		overdueDays = 0
	}

	// Priority increases with: overdue time, low EF (hard items), low repetition count
	priority := math.Min(1.0, (overdueDays+1)/30.0) // max out at 30 days overdue

	// Harder items (lower EF) get higher priority
	efFactor := (2.5 - s.EasinessFactor) / 1.2 // normalize to [0, 1]
	priority = 0.6*priority + 0.4*efFactor

	// Items with fewer repetitions are more urgent
	repFactor := 1.0 - math.Min(1.0, float64(s.RepetitionCount)/10.0)
	priority = 0.7*priority + 0.3*repFactor

	return math.Min(1.0, priority)
}

// Sm2FromProperties extracts SM2Data from node properties map.
// Returns DefaultSM2Data() if no SM2 data exists in properties.
func Sm2FromProperties(props map[string]any) SM2Data {
	data := DefaultSM2Data()

	if props == nil {
		return data
	}

	if ef, ok := props["sm2_easiness_factor"].(float64); ok {
		data.EasinessFactor = ef
	}
	if interval, ok := props["sm2_interval_days"].(int); ok {
		data.IntervalDays = interval
	} else if intervalF, ok := props["sm2_interval_days"].(float64); ok {
		data.IntervalDays = int(intervalF)
	}
	if reps, ok := props["sm2_repetition_count"].(int); ok {
		data.RepetitionCount = reps
	} else if repsF, ok := props["sm2_repetition_count"].(float64); ok {
		data.RepetitionCount = int(repsF)
	}
	if quality, ok := props["sm2_last_quality"].(int); ok {
		data.LastQuality = quality
	} else if qualityF, ok := props["sm2_last_quality"].(float64); ok {
		data.LastQuality = int(qualityF)
	}
	if nextReview, ok := props["sm2_next_review"].(string); ok {
		if t, err := time.Parse(time.RFC3339, nextReview); err == nil {
			data.NextReviewDate = t
		}
	}

	return data
}

// ToProperties stores SM2Data into a properties map.
// Modifies the map in place (creates if nil).
func (s SM2Data) ToProperties(props map[string]any) map[string]any {
	if props == nil {
		props = make(map[string]any)
	}

	props["sm2_easiness_factor"] = s.EasinessFactor
	props["sm2_interval_days"] = s.IntervalDays
	props["sm2_repetition_count"] = s.RepetitionCount
	props["sm2_last_quality"] = s.LastQuality
	props["sm2_next_review"] = s.NextReviewDate.Format(time.RFC3339)

	return props
}
