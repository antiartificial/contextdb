package core

import (
	"math"
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestDefaultSM2Data(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()

	is.Equal(sm2.EasinessFactor, 2.5)
	is.Equal(sm2.IntervalDays, 0)
	is.Equal(sm2.RepetitionCount, 0)
	is.True(sm2.NextReviewDate.IsZero())
	is.Equal(sm2.LastQuality, 0)
}

func TestSM2IsDue(t *testing.T) {
	is := is.New(t)

	// Not yet scheduled
	sm2 := DefaultSM2Data()
	is.True(sm2.IsDue(time.Now()))

	// Scheduled for future
	sm2.NextReviewDate = time.Now().Add(24 * time.Hour)
	is.True(!sm2.IsDue(time.Now()))

	// Overdue
	sm2.NextReviewDate = time.Now().Add(-1 * time.Hour)
	is.True(sm2.IsDue(time.Now()))
}

func TestSM2Update_Quality5(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()
	newSM2 := sm2.Update(5)

	// First repetition with perfect quality
	is.Equal(newSM2.RepetitionCount, 1)
	is.Equal(newSM2.IntervalDays, 1)
	is.True(newSM2.EasinessFactor > 2.5) // EF should increase
	is.True(!newSM2.NextReviewDate.IsZero())
}

func TestSM2Update_Quality3(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()
	newSM2 := sm2.Update(3)

	// First repetition with passing quality
	is.Equal(newSM2.RepetitionCount, 1)
	is.Equal(newSM2.IntervalDays, 1)
	is.True(newSM2.EasinessFactor < 2.5) // EF should decrease for quality 3
}

func TestSM2Update_Failure(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()

	// First successful review
	sm2 = sm2.Update(4)
	is.Equal(sm2.RepetitionCount, 1)
	is.Equal(sm2.IntervalDays, 1)

	// Second successful review
	sm2 = sm2.Update(4)
	is.Equal(sm2.RepetitionCount, 2)
	is.Equal(sm2.IntervalDays, 6)

	// Third successful review - interval grows by EF
	sm2 = sm2.Update(4)
	is.Equal(sm2.RepetitionCount, 3)
	is.True(sm2.IntervalDays > 6)

	// Now fail (quality 2)
	sm2 = sm2.Update(2)

	is.Equal(sm2.RepetitionCount, 0) // Reset
	is.Equal(sm2.IntervalDays, 1)    // Back to 1 day
	is.True(sm2.EasinessFactor < 2.5) // EF decreased
}

func TestSM2Update_ClampedQuality(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()

	// Quality > 5 should be clamped
	newSM2 := sm2.Update(10)
	is.Equal(newSM2.LastQuality, 5)

	// Quality < 0 should be clamped
	newSM2 = sm2.Update(-5)
	is.Equal(newSM2.LastQuality, 0)
}

func TestSM2Update_EFMinimum(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()

	// Repeatedly fail to drive EF down
	for i := 0; i < 20; i++ {
		sm2 = sm2.Update(0)
	}

	// EF should not go below 1.3
	is.True(sm2.EasinessFactor >= 1.3)
}

func TestSM2IntervalProgression(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()

	// Simulate 10 successful reviews with quality 4
	intervals := []int{}
	for i := 0; i < 10; i++ {
		sm2 = sm2.Update(4)
		intervals = append(intervals, sm2.IntervalDays)
	}

	// First interval should be 1
	is.Equal(intervals[0], 1)
	// Second interval should be 6
	is.Equal(intervals[1], 6)
	// Subsequent intervals should grow
	for i := 2; i < len(intervals); i++ {
		is.True(intervals[i] > intervals[i-1])
	}
}

func TestPriorityScore(t *testing.T) {
	is := is.New(t)

	now := time.Now()

	// Not due
	sm2 := DefaultSM2Data()
	sm2.NextReviewDate = now.Add(24 * time.Hour)
	is.Equal(sm2.PriorityScore(now), 0.0)

	// Due now
	sm2.NextReviewDate = now
	is.True(sm2.PriorityScore(now) > 0)

	// Overdue by 30 days - should be high priority
	sm2.NextReviewDate = now.Add(-30 * 24 * time.Hour)
	highPriority := sm2.PriorityScore(now)
	is.True(highPriority > 0.7)
}

func TestSm2FromProperties(t *testing.T) {
	is := is.New(t)

	props := map[string]any{
		"sm2_easiness_factor": 2.8,
		"sm2_interval_days":     14,
		"sm2_repetition_count":  5,
		"sm2_last_quality":    4,
		"sm2_next_review":     time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}

	sm2 := Sm2FromProperties(props)

	is.Equal(sm2.EasinessFactor, 2.8)
	is.Equal(sm2.IntervalDays, 14)
	is.Equal(sm2.RepetitionCount, 5)
	is.Equal(sm2.LastQuality, 4)
	is.True(!sm2.NextReviewDate.IsZero())
}

func TestSm2FromProperties_Defaults(t *testing.T) {
	is := is.New(t)

	sm2 := Sm2FromProperties(nil)
	is.Equal(sm2.EasinessFactor, 2.5)

	sm2 = Sm2FromProperties(map[string]any{})
	is.Equal(sm2.EasinessFactor, 2.5)
}

func TestSM2ToProperties(t *testing.T) {
	is := is.New(t)

	sm2 := DefaultSM2Data()
	sm2.EasinessFactor = 2.2
	sm2.IntervalDays = 10
	sm2.RepetitionCount = 3
	sm2.LastQuality = 4
	sm2.NextReviewDate = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	props := sm2.ToProperties(nil)

	is.Equal(props["sm2_easiness_factor"], 2.2)
	is.Equal(props["sm2_interval_days"], 10)
	is.Equal(props["sm2_repetition_count"], 3)
	is.Equal(props["sm2_last_quality"], 4)
	is.Equal(props["sm2_next_review"], "2024-01-15T12:00:00Z")
}

func TestSM2RoundTrip(t *testing.T) {
	is := is.New(t)

	original := DefaultSM2Data()
	original.EasinessFactor = 2.1
	original.IntervalDays = 15
	original.RepetitionCount = 7
	original.LastQuality = 3
	// Use a fixed timestamp to avoid parsing issues
	original.NextReviewDate = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	props := original.ToProperties(nil)
	recovered := Sm2FromProperties(props)

	is.True(math.Abs(original.EasinessFactor-recovered.EasinessFactor) < 0.001)
	is.Equal(original.IntervalDays, recovered.IntervalDays)
	is.Equal(original.RepetitionCount, recovered.RepetitionCount)
	is.Equal(original.LastQuality, recovered.LastQuality)
	// Allow 1 second tolerance for time parsing
	is.True(math.Abs(float64(original.NextReviewDate.Unix()-recovered.NextReviewDate.Unix())) < 2)
}

func TestSM2Formula(t *testing.T) {
	is := is.New(t)

	// Test EF update formula for various qualities
	testCases := []struct {
		currentEF float64
		quality   int
		minEF     float64
		maxEF     float64
	}{
		{2.5, 5, 2.5, 2.7},      // perfect answer, EF increases slightly
		{2.5, 4, 2.4, 2.6},      // good answer, EF stays roughly same
		{2.5, 3, 2.3, 2.5},      // pass, EF decreases
		{2.5, 2, 2.1, 2.4},      // fail, EF decreases more
		{2.5, 0, 1.6, 2.0},      // blackout, EF decreases significantly (clamped at 1.3 min)
		{1.3, 0, 1.3, 1.3},      // minimum clamp
	}

	for _, tc := range testCases {
		sm2 := DefaultSM2Data()
		sm2.EasinessFactor = tc.currentEF
		newSM2 := sm2.Update(tc.quality)

		is.True(newSM2.EasinessFactor >= tc.minEF)
		is.True(newSM2.EasinessFactor <= tc.maxEF)
	}
}
