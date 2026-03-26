package longmemeval_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/bench/longmemeval"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestRunner_SyntheticDataset(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{})
	defer db.Close()

	dataset := longmemeval.GenerateSyntheticDataset()
	is.True(len(dataset.Sessions) > 0)  // dataset has sessions
	is.True(len(dataset.Queries) > 0)   // dataset has queries

	runner := longmemeval.NewRunner(db, longmemeval.Config{
		TopK:      5,
		VectorDim: 8,
	})

	report, err := runner.Run(ctx, dataset)
	is.NoErr(err)
	is.True(report != nil) // report should not be nil

	// Structural checks.
	is.Equal(report.TotalQueries, len(dataset.Queries))
	is.Equal(len(report.Results), len(dataset.Queries))
	is.True(len(report.ByCategory) > 0) // at least one category

	// Every result should have a query ID and non-empty retrieved list.
	for _, res := range report.Results {
		is.True(res.QueryID != "")            // every result has a query ID
		is.True(res.Category != "")           // every result has a category
		is.True(res.Latency > 0)              // latency must be positive
		is.True(res.RecallAt1 >= 0)           // recall is non-negative
		is.True(res.RecallAt5 >= res.RecallAt1) // R@5 >= R@1
	}

	// Verify all three categories appear.
	_, hasSingle := report.ByCategory["single-session"]
	_, hasMulti := report.ByCategory["multi-session"]
	_, hasTemporal := report.ByCategory["temporal"]
	is.True(hasSingle)   // single-session category present
	is.True(hasMulti)    // multi-session category present
	is.True(hasTemporal) // temporal category present

	// Mean recall should be non-negative.
	is.True(report.MeanRecallAt1 >= 0)
	is.True(report.MeanRecallAt5 >= 0)
	is.True(report.MeanLatency > 0) // at least some latency

	// Print the report for human review.
	runner.PrintReport(report)

	t.Logf("Mean R@1: %.1f%%  Mean R@5: %.1f%%  Mean latency: %s",
		report.MeanRecallAt1*100, report.MeanRecallAt5*100, report.MeanLatency)
}

func TestGenerateSyntheticDataset_Structure(t *testing.T) {
	is := is.New(t)

	ds := longmemeval.GenerateSyntheticDataset()

	// 10 sessions, each with 3-5 turns.
	is.Equal(len(ds.Sessions), 10)
	for _, s := range ds.Sessions {
		is.True(s.ID != "")           // session has ID
		is.True(len(s.Turns) >= 3)    // at least 3 turns
		is.True(len(s.Turns) <= 5)    // at most 5 turns
		for _, turn := range s.Turns {
			is.True(turn.Role == "user" || turn.Role == "assistant")
			is.True(turn.Content != "") // turn has content
		}
	}

	// 10 queries across 3 categories.
	is.Equal(len(ds.Queries), 10)
	categories := make(map[string]int)
	for _, q := range ds.Queries {
		is.True(q.ID != "")
		is.True(q.Question != "")
		is.True(q.GoldAnswer != "")
		is.True(q.Category != "")
		is.True(len(q.RequiredSessions) > 0) // at least one required session
		categories[q.Category]++
	}
	is.True(categories["single-session"] > 0)
	is.True(categories["multi-session"] > 0)
	is.True(categories["temporal"] > 0)
}
