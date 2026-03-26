package mteb_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/bench/mteb"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestRunner_RetrievalSuite(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{})
	defer db.Close()

	suite := mteb.BuildRetrievalSuite()
	is.True(len(suite.Documents) > 0)
	is.True(len(suite.Queries) > 0)

	runner := mteb.NewRunner(db, mteb.Config{
		TopK:      10,
		VectorDim: 8,
	})

	report, err := runner.Run(ctx, suite)
	is.NoErr(err)
	is.True(report != nil)

	// Structural checks.
	is.Equal(report.TotalQueries, len(suite.Queries))
	is.Equal(len(report.Results), len(suite.Queries))
	is.True(report.SuiteName == "contextdb-retrieval-v1")

	for _, res := range report.Results {
		is.True(res.QueryID != "")
		is.True(res.Latency > 0)
		is.True(res.RecallAt1 >= 0)
		is.True(res.RecallAt10 >= res.RecallAt5)
		is.True(res.RecallAt5 >= res.RecallAt1)
		is.True(res.NDCG10 >= 0)
		is.True(res.MRR >= 0)
	}

	// Overall metrics.
	is.True(report.MeanNDCG10 >= 0)
	is.True(report.MeanRecall1 >= 0)
	is.True(report.MeanMRR >= 0)
	is.True(report.MeanLatency > 0)

	// Print report for human review.
	runner.PrintReport(report)

	t.Logf("MTEB suite: NDCG@10=%.3f  R@1=%.1f%%  R@5=%.1f%%  R@10=%.1f%%  MRR=%.3f",
		report.MeanNDCG10,
		report.MeanRecall1*100,
		report.MeanRecall5*100,
		report.MeanRecall10*100,
		report.MeanMRR)
}

func TestBuildRetrievalSuite_Structure(t *testing.T) {
	is := is.New(t)

	suite := mteb.BuildRetrievalSuite()

	// Should have documents across 5 clusters.
	is.True(len(suite.Documents) >= 20)

	groupCounts := make(map[string]int)
	for _, doc := range suite.Documents {
		is.True(doc.ID != "")
		is.True(doc.Text != "")
		is.True(len(doc.Vector) == 8)
		is.True(len(doc.Labels) > 0)
		groupCounts[doc.GroupID]++
	}
	is.Equal(len(groupCounts), 5) // 5 clusters

	// Should have 12 queries.
	is.True(len(suite.Queries) >= 10)
	for _, q := range suite.Queries {
		is.True(q.ID != "")
		is.True(q.Text != "")
		is.True(len(q.Vector) == 8)
		is.True(len(q.Relevant) > 0)
	}
}

func TestNDCG_EdgeCases(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{})
	defer db.Close()

	// Test with a minimal suite: one document, one query.
	suite := &mteb.Suite{
		Name: "edge-case",
		Documents: []mteb.Document{
			{
				ID:      "d1",
				Text:    "test document",
				Vector:  []float32{1, 0, 0, 0, 0, 0, 0, 0},
				Labels:  []string{"Test"},
				GroupID: "test",
			},
		},
		Queries: []mteb.RetrievalQuery{
			{
				ID:       "q1",
				Text:     "test query",
				Vector:   []float32{1, 0, 0, 0, 0, 0, 0, 0},
				Relevant: []string{"d1"},
			},
		},
	}

	runner := mteb.NewRunner(db, mteb.Config{
		Namespace: "mteb-edge",
		TopK:      5,
	})

	report, err := runner.Run(ctx, suite)
	is.NoErr(err)
	is.Equal(report.TotalQueries, 1)

	// Single relevant document should yield perfect scores.
	is.True(report.MeanRecall1 > 0)   // should find the doc
	is.True(report.MeanNDCG10 > 0)    // non-zero NDCG
	is.True(report.MeanMRR > 0)       // non-zero MRR
}
