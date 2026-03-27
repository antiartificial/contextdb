package retrieval

// GapReport is the structured output of a knowledge gap analysis.
type GapReport struct {
	Namespace     string
	Gaps          []KnowledgeGap
	CoverageScore float64 // [0, 1]: 1 = fully covered, 0 = mostly gaps
	TotalNodes    int
	GapsDetected  int
}

// BuildGapReport creates a summary report from detected gaps and namespace stats.
func BuildGapReport(ns string, gaps []KnowledgeGap, totalNodes int) *GapReport {
	report := &GapReport{
		Namespace:    ns,
		Gaps:         gaps,
		TotalNodes:   totalNodes,
		GapsDetected: len(gaps),
	}

	// Coverage score: fewer gaps relative to total nodes = better coverage
	if totalNodes == 0 {
		report.CoverageScore = 0
	} else if len(gaps) == 0 {
		report.CoverageScore = 1.0
	} else {
		// Heuristic: coverage decreases with gap count, scaled by total nodes
		gapRatio := float64(len(gaps)) / float64(totalNodes)
		report.CoverageScore = 1.0 - gapRatio
		if report.CoverageScore < 0 {
			report.CoverageScore = 0
		}
	}

	return report
}
