package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/testdata"
	"github.com/google/uuid"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func runEval(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb eval: expected ranking")
		os.Exit(2)
	}
	switch args[0] {
	case "ranking":
		runEvalRanking(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb eval: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runEvalRanking(args []string) {
	if len(args) > 0 && args[0] == "baseline" {
		runEvalRankingBaseline(args[1:])
		return
	}
	fs := flag.NewFlagSet("contextdb eval ranking", flag.ExitOnError)
	outPath := fs.String("out", "", "JSON ranking eval snapshot to write")
	markdownOutPath := fs.String("markdown-out", "", "Markdown ranking eval recap to write")
	comparePath := fs.String("compare", "", "previous JSON ranking eval snapshot to compare")
	baselineDir := fs.String("baseline-dir", "", "directory for versioned ranking eval baseline artifacts")
	compareBaselineDir := fs.String("compare-baseline-dir", "", "directory containing previous versioned ranking eval baselines")
	baselineRetentionDir := fs.String("baseline-retention-dir", "", "directory containing versioned ranking eval baselines to inspect")
	baselineRetentionKeep := fs.Int("baseline-retention-keep", 5, "number of newest ranking eval baseline versions to retain")
	emitDeleteScript := fs.Bool("emit-delete-script", false, "print a shell script for pruneable ranking eval baselines without deleting files")
	baselineManifestOut := fs.String("baseline-manifest-out", "", "write a JSON manifest for ranking eval baseline artifacts")
	diffOutPath := fs.String("diff-out", "", "JSON ranking eval diff to write")
	diffMarkdownOutPath := fs.String("diff-markdown-out", "", "Markdown ranking eval diff to write")
	reportOut := fs.Bool("report", false, "print the JSON ranking eval snapshot")
	markdownReportOut := fs.Bool("markdown", false, "print the Markdown ranking eval recap")
	diffReportOut := fs.Bool("diff-report", false, "print the JSON ranking eval diff")
	diffMarkdownReportOut := fs.Bool("diff-markdown", false, "print the Markdown ranking eval diff")
	topK := fs.Int("top-k", 5, "number of ranked results to include per query")
	_ = fs.Parse(args)

	if strings.TrimSpace(*baselineRetentionDir) != "" {
		report, err := buildRankingEvalBaselineRetentionReport(*baselineRetentionDir, *baselineRetentionKeep)
		if err == nil && strings.TrimSpace(*baselineManifestOut) != "" {
			manifest, manifestErr := buildRankingEvalBaselineArtifactManifest(report, time.Now())
			if manifestErr != nil {
				err = manifestErr
			} else if writeErr := writeJSONFile(*baselineManifestOut, manifest); writeErr != nil {
				err = writeErr
			}
		}
		if *emitDeleteScript {
			fmt.Print(buildRankingEvalBaselineDeleteScript(report))
		} else {
			writeIndentedJSON(report)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
		return
	}

	report, err := buildRankingEvalSnapshotReport(context.Background(), rankingEvalSnapshotOptions{
		TopK:        *topK,
		GeneratedAt: time.Now(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := writeJSONFile(*outPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
	}
	if strings.TrimSpace(*markdownOutPath) != "" {
		if err := writeTextFile(*markdownOutPath, buildRankingEvalMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
	}
	if strings.TrimSpace(*baselineDir) != "" {
		if _, err := writeRankingEvalBaselineArtifacts(*baselineDir, report); err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
	}
	if *markdownReportOut {
		fmt.Print(buildRankingEvalMarkdown(report))
	}

	resolvedComparePath := strings.TrimSpace(*comparePath)
	resolvedCompareBaselineDir := strings.TrimSpace(*compareBaselineDir)
	if resolvedComparePath != "" && resolvedCompareBaselineDir != "" {
		fmt.Fprintln(os.Stderr, "contextdb eval ranking: --compare and --compare-baseline-dir are mutually exclusive")
		os.Exit(2)
	}
	if resolvedComparePath == "" && resolvedCompareBaselineDir != "" {
		resolved, err := resolveRankingEvalBaselineComparePath(resolvedCompareBaselineDir, buildinfo.Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
		resolvedComparePath = resolved
	}
	diffRequested := resolvedComparePath != ""
	if diffRequested {
		previous, err := readRankingEvalSnapshotReport(resolvedComparePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
			os.Exit(1)
		}
		diff := buildRankingEvalDiffReport(previous, report)
		if strings.TrimSpace(*diffOutPath) != "" {
			if err := writeJSONFile(*diffOutPath, diff); err != nil {
				fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
				os.Exit(1)
			}
		}
		if strings.TrimSpace(*diffMarkdownOutPath) != "" {
			if err := writeTextFile(*diffMarkdownOutPath, buildRankingEvalDiffMarkdown(diff)); err != nil {
				fmt.Fprintf(os.Stderr, "contextdb eval ranking: %v\n", err)
				os.Exit(1)
			}
		}
		if *diffMarkdownReportOut {
			fmt.Print(buildRankingEvalDiffMarkdown(diff))
		}
		if *diffReportOut || (strings.TrimSpace(*outPath) == "" && !*reportOut && strings.TrimSpace(*markdownOutPath) == "" && strings.TrimSpace(*baselineDir) == "" && !*markdownReportOut && strings.TrimSpace(*diffOutPath) == "" && strings.TrimSpace(*diffMarkdownOutPath) == "" && !*diffMarkdownReportOut) {
			writeIndentedJSON(diff)
		}
	}
	if *reportOut || (!diffRequested && strings.TrimSpace(*outPath) == "" && strings.TrimSpace(*markdownOutPath) == "" && strings.TrimSpace(*baselineDir) == "" && !*markdownReportOut) {
		writeIndentedJSON(report)
	}
}

func runEvalRankingBaseline(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb eval ranking baseline: expected manifest")
		os.Exit(2)
	}
	switch args[0] {
	case "manifest":
		runEvalRankingBaselineManifest(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runEvalRankingBaselineManifest(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb eval ranking baseline manifest: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runEvalRankingBaselineManifestVerify(args[1:])
	case "bundle":
		runEvalRankingBaselineManifestBundle(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runEvalRankingBaselineManifestVerify(args []string) {
	fs := flag.NewFlagSet("contextdb eval ranking baseline manifest verify", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "JSON ranking baseline artifact manifest to verify")
	reportOut := fs.Bool("report", false, "print a JSON ranking baseline manifest verification report")
	markdownOut := fs.Bool("markdown", false, "print a Markdown ranking baseline manifest verification summary")
	markdownOutPath := fs.String("markdown-out", "", "Markdown ranking baseline manifest verification summary to write")
	annotationsOut := fs.Bool("annotations", false, "print CI annotation lines for ranking baseline manifest verification failures")
	annotationsOutPath := fs.String("annotations-out", "", "CI annotation lines for ranking baseline manifest verification failures to write")
	bundleDir := fs.String("bundle-dir", "", "directory for JSON, Markdown, and annotation verification artifacts")
	_ = fs.Parse(args)

	report, err := verifyRankingEvalBaselineArtifactManifest(*manifestPath)
	if strings.TrimSpace(*bundleDir) != "" {
		if writeErr := writeRankingEvalBaselineArtifactManifestVerifyBundle(*bundleDir, report); writeErr != nil && err == nil {
			err = writeErr
		}
	}
	if strings.TrimSpace(*markdownOutPath) != "" {
		if writeErr := writeTextFile(*markdownOutPath, buildRankingEvalBaselineArtifactManifestVerifyMarkdown(report)); writeErr != nil && err == nil {
			err = writeErr
		}
	}
	if strings.TrimSpace(*annotationsOutPath) != "" {
		if writeErr := writeTextFile(*annotationsOutPath, buildRankingEvalBaselineArtifactManifestFailureAnnotations(report)); writeErr != nil && err == nil {
			err = writeErr
		}
	}
	if *markdownOut {
		fmt.Print(buildRankingEvalBaselineArtifactManifestVerifyMarkdown(report))
	}
	if *annotationsOut {
		annotations := buildRankingEvalBaselineArtifactManifestFailureAnnotations(report)
		if strings.TrimSpace(annotations) == "" {
			fmt.Fprintln(os.Stdout, "ok")
		} else {
			fmt.Print(annotations)
		}
	}
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut && !*markdownOut && strings.TrimSpace(*markdownOutPath) == "" && !*annotationsOut && strings.TrimSpace(*annotationsOutPath) == "" && strings.TrimSpace(*bundleDir) == "" {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runEvalRankingBaselineManifestBundle(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb eval ranking baseline manifest bundle: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runEvalRankingBaselineManifestBundleVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest bundle: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runEvalRankingBaselineManifestBundleVerify(args []string) {
	fs := flag.NewFlagSet("contextdb eval ranking baseline manifest bundle verify", flag.ExitOnError)
	indexPath := fs.String("index", "", "ranking baseline verification bundle index JSON to verify")
	reportOut := fs.Bool("report", false, "print a JSON ranking baseline verification bundle report")
	_ = fs.Parse(args)

	report, err := verifyRankingEvalBaselineArtifactManifestVerifyBundleIndex(*indexPath)
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest bundle verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

type rankingEvalSnapshotOptions struct {
	TopK        int
	GeneratedAt time.Time
}

type rankingEvalSnapshotReport struct {
	SchemaVersion    int                        `json:"schema_version"`
	GeneratedAt      string                     `json:"generated_at"`
	ContextDBVersion string                     `json:"contextdb_version"`
	Corpus           string                     `json:"corpus"`
	TopK             int                        `json:"top_k"`
	TotalQueries     int                        `json:"total_queries"`
	PassedQueries    int                        `json:"passed_queries"`
	FailedQueries    int                        `json:"failed_queries"`
	MeanReciprocal   float64                    `json:"mean_reciprocal_rank"`
	Queries          []rankingEvalSnapshotQuery `json:"queries"`
}

type rankingEvalSnapshotQuery struct {
	ID                 string                      `json:"id"`
	Description        string                      `json:"description"`
	Namespace          string                      `json:"namespace"`
	Category           string                      `json:"category"`
	ExpectedRankCutoff int                         `json:"expected_rank_cutoff"`
	CorrectRank        int                         `json:"correct_rank,omitempty"`
	ReciprocalRank     float64                     `json:"reciprocal_rank"`
	Passed             bool                        `json:"passed"`
	TopResults         []rankingEvalSnapshotResult `json:"top_results"`
}

type rankingEvalSnapshotResult struct {
	Rank            int                 `json:"rank"`
	NodeID          string              `json:"node_id"`
	Text            string              `json:"text,omitempty"`
	Expected        bool                `json:"expected"`
	Score           float64             `json:"score"`
	SimilarityScore float64             `json:"similarity_score"`
	ConfidenceScore float64             `json:"confidence_score"`
	RecencyScore    float64             `json:"recency_score"`
	UtilityScore    float64             `json:"utility_score"`
	ScoreBreakdown  core.ScoreBreakdown `json:"score_breakdown"`
	RetrievalSource string              `json:"retrieval_source,omitempty"`
}

type rankingEvalDiffReport struct {
	SchemaVersion          int                    `json:"schema_version"`
	ContextDBVersion       string                 `json:"contextdb_version"`
	PreviousGeneratedAt    string                 `json:"previous_generated_at"`
	CurrentGeneratedAt     string                 `json:"current_generated_at"`
	Corpus                 string                 `json:"corpus"`
	TopK                   int                    `json:"top_k"`
	TotalQueries           int                    `json:"total_queries"`
	ComparedQueries        int                    `json:"compared_queries"`
	MissingPreviousQueries []string               `json:"missing_previous_queries,omitempty"`
	MissingCurrentQueries  []string               `json:"missing_current_queries,omitempty"`
	PreviousMRR            float64                `json:"previous_mean_reciprocal_rank"`
	CurrentMRR             float64                `json:"current_mean_reciprocal_rank"`
	MRRDelta               float64                `json:"mean_reciprocal_rank_delta"`
	PreviousPassedQueries  int                    `json:"previous_passed_queries"`
	CurrentPassedQueries   int                    `json:"current_passed_queries"`
	PassedDelta            int                    `json:"passed_delta"`
	PassChangedQueries     []string               `json:"pass_changed_queries,omitempty"`
	LargestRankMovements   []rankingEvalDiffQuery `json:"largest_rank_movements"`
	LargestScoreMovements  []rankingEvalDiffQuery `json:"largest_score_movements"`
	Queries                []rankingEvalDiffQuery `json:"queries"`
}

type rankingEvalDiffQuery struct {
	ID                     string  `json:"id"`
	Category               string  `json:"category"`
	PreviousPassed         bool    `json:"previous_passed"`
	CurrentPassed          bool    `json:"current_passed"`
	PreviousCorrectRank    int     `json:"previous_correct_rank,omitempty"`
	CurrentCorrectRank     int     `json:"current_correct_rank,omitempty"`
	RankDelta              int     `json:"rank_delta"`
	PreviousReciprocalRank float64 `json:"previous_reciprocal_rank"`
	CurrentReciprocalRank  float64 `json:"current_reciprocal_rank"`
	ReciprocalRankDelta    float64 `json:"reciprocal_rank_delta"`
	PreviousTopNodeID      string  `json:"previous_top_node_id,omitempty"`
	CurrentTopNodeID       string  `json:"current_top_node_id,omitempty"`
	PreviousTopText        string  `json:"previous_top_text,omitempty"`
	CurrentTopText         string  `json:"current_top_text,omitempty"`
	PreviousTopScore       float64 `json:"previous_top_score"`
	CurrentTopScore        float64 `json:"current_top_score"`
	TopScoreDelta          float64 `json:"top_score_delta"`
	TopResultChanged       bool    `json:"top_result_changed"`
}

type rankingEvalBaselineArtifacts struct {
	Version      string `json:"version"`
	Dir          string `json:"dir"`
	JSONPath     string `json:"json_path"`
	MarkdownPath string `json:"markdown_path"`
}

type rankingEvalBaselineRetentionReport struct {
	OK                bool                               `json:"ok"`
	Dir               string                             `json:"dir"`
	Keep              int                                `json:"keep"`
	TotalVersions     int                                `json:"total_versions"`
	RetainedVersions  int                                `json:"retained_versions"`
	PruneableVersions int                                `json:"pruneable_versions"`
	DeleteCommands    []string                           `json:"delete_commands,omitempty"`
	ValidationErrors  []string                           `json:"validation_errors,omitempty"`
	Baselines         []rankingEvalBaselineRetentionItem `json:"baselines"`
}

type rankingEvalBaselineRetentionItem struct {
	Version      string   `json:"version"`
	Current      bool     `json:"current"`
	Status       string   `json:"status"`
	JSONPath     string   `json:"json_path,omitempty"`
	MarkdownPath string   `json:"markdown_path,omitempty"`
	Missing      []string `json:"missing,omitempty"`
}

type rankingEvalBaselineArtifactManifest struct {
	Kind              string                                    `json:"kind"`
	SchemaVersion     int                                       `json:"schema_version"`
	GeneratedAt       string                                    `json:"generated_at"`
	ContextDBVersion  string                                    `json:"contextdb_version"`
	Dir               string                                    `json:"dir"`
	Keep              int                                       `json:"keep"`
	TotalVersions     int                                       `json:"total_versions"`
	RetainedVersions  int                                       `json:"retained_versions"`
	PruneableVersions int                                       `json:"pruneable_versions"`
	Artifacts         []rankingEvalBaselineArtifactManifestItem `json:"artifacts"`
}

type rankingEvalBaselineArtifactManifestItem struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Current bool   `json:"current"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Bytes   int64  `json:"bytes,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type rankingEvalBaselineArtifactManifestVerifyReport struct {
	OK                    bool                                                  `json:"ok"`
	ManifestFile          string                                                `json:"manifest_file"`
	ManifestKind          string                                                `json:"manifest_kind,omitempty"`
	ManifestGeneratedAt   string                                                `json:"manifest_generated_at,omitempty"`
	ContextDBVersion      string                                                `json:"contextdb_version,omitempty"`
	TotalArtifacts        int                                                   `json:"total_artifacts"`
	VerifiedArtifacts     int                                                   `json:"verified_artifacts"`
	MissingArtifacts      int                                                   `json:"missing_artifacts"`
	ValidationErrors      []string                                              `json:"validation_errors,omitempty"`
	ArtifactVerifications []rankingEvalBaselineArtifactManifestVerifyReportItem `json:"artifact_verifications"`
}

type rankingEvalBaselineArtifactManifestVerifyReportItem struct {
	Version          string   `json:"version"`
	Status           string   `json:"status"`
	Current          bool     `json:"current"`
	Kind             string   `json:"kind"`
	Path             string   `json:"path"`
	ExpectedMissing  bool     `json:"expected_missing,omitempty"`
	Exists           bool     `json:"exists"`
	ExpectedBytes    int64    `json:"expected_bytes,omitempty"`
	ActualBytes      int64    `json:"actual_bytes,omitempty"`
	ExpectedSHA256   string   `json:"expected_sha256,omitempty"`
	ActualSHA256     string   `json:"actual_sha256,omitempty"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
}

type rankingEvalBaselineArtifactManifestVerifyBundleIndex struct {
	Kind             string                                                         `json:"kind"`
	SchemaVersion    int                                                            `json:"schema_version"`
	GeneratedAt      string                                                         `json:"generated_at"`
	Status           string                                                         `json:"status"`
	OK               bool                                                           `json:"ok"`
	ManifestFile     string                                                         `json:"manifest_file"`
	ContextDBVersion string                                                         `json:"contextdb_version,omitempty"`
	Artifacts        []rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact `json:"artifacts"`
}

type rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReport struct {
	OK                    bool                                                                   `json:"ok"`
	IndexFile             string                                                                 `json:"index_file"`
	IndexKind             string                                                                 `json:"index_kind,omitempty"`
	SchemaVersion         int                                                                    `json:"schema_version,omitempty"`
	GeneratedAt           string                                                                 `json:"generated_at,omitempty"`
	Status                string                                                                 `json:"status,omitempty"`
	BundleOK              bool                                                                   `json:"bundle_ok"`
	ManifestFile          string                                                                 `json:"manifest_file,omitempty"`
	ContextDBVersion      string                                                                 `json:"contextdb_version,omitempty"`
	TotalArtifacts        int                                                                    `json:"total_artifacts"`
	VerifiedArtifacts     int                                                                    `json:"verified_artifacts"`
	ValidationErrors      []string                                                               `json:"validation_errors,omitempty"`
	ArtifactVerifications []rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReportItem `json:"artifact_verifications"`
}

type rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReportItem struct {
	Kind             string   `json:"kind"`
	Path             string   `json:"path"`
	ResolvedPath     string   `json:"resolved_path,omitempty"`
	Exists           bool     `json:"exists"`
	ExpectedBytes    int64    `json:"expected_bytes,omitempty"`
	ActualBytes      int64    `json:"actual_bytes,omitempty"`
	ExpectedSHA256   string   `json:"expected_sha256,omitempty"`
	ActualSHA256     string   `json:"actual_sha256,omitempty"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
}

type rankingEvalBaselineVersion struct {
	Path  string
	Major int
	Minor int
	Patch int
}

func buildRankingEvalSnapshotReport(ctx context.Context, opts rankingEvalSnapshotOptions) (rankingEvalSnapshotReport, error) {
	topK := opts.TopK
	if topK <= 0 {
		topK = 5
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	corpus := testdata.Build()
	engine := retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}
	report := rankingEvalSnapshotReport{
		SchemaVersion:    1,
		GeneratedAt:      generatedAt.UTC().Format(time.RFC3339),
		ContextDBVersion: buildinfo.Version,
		Corpus:           "representative",
		TopK:             topK,
		TotalQueries:     len(corpus.QuerySet),
	}
	reciprocalSum := 0.0
	for _, query := range corpus.QuerySet {
		cfg := namespace.Defaults(query.Namespace, rankingEvalCorpusMode(query.Namespace))
		results, err := engine.Retrieve(ctx, retrieval.Query{
			Namespace:   query.Namespace,
			Vector:      query.Vector,
			TopK:        topK,
			Strategy:    retrieval.HybridStrategy{VectorWeight: 1, Traversal: cfg.Traversal, MaxDepth: cfg.MaxDepth},
			ScoreParams: cfg.ScoreParams,
		})
		if err != nil {
			return report, fmt.Errorf("ranking eval %s: %w", query.ID, err)
		}
		queryReport := rankingEvalSnapshotQuery{
			ID:                 query.ID,
			Description:        query.Description,
			Namespace:          query.Namespace,
			Category:           query.Category,
			ExpectedRankCutoff: rankingEvalExpectedRankCutoff(query.Category),
		}
		if queryReport.ExpectedRankCutoff > len(results) {
			queryReport.ExpectedRankCutoff = len(results)
		}
		for i, result := range results {
			rank := i + 1
			expected := rankingEvalContainsNode(query.CorrectNodeIDs, result.Node.ID)
			if expected && queryReport.CorrectRank == 0 {
				queryReport.CorrectRank = rank
				queryReport.ReciprocalRank = 1 / float64(rank)
				reciprocalSum += queryReport.ReciprocalRank
			}
			text, _ := result.Node.Properties["text"].(string)
			queryReport.TopResults = append(queryReport.TopResults, rankingEvalSnapshotResult{
				Rank:            rank,
				NodeID:          result.Node.ID.String(),
				Text:            text,
				Expected:        expected,
				Score:           result.Score,
				SimilarityScore: result.SimilarityScore,
				ConfidenceScore: result.ConfidenceScore,
				RecencyScore:    result.RecencyScore,
				UtilityScore:    result.UtilityScore,
				ScoreBreakdown:  result.Breakdown,
				RetrievalSource: result.RetrievalSource,
			})
		}
		queryReport.Passed = queryReport.CorrectRank > 0 && queryReport.CorrectRank <= queryReport.ExpectedRankCutoff
		if queryReport.Passed {
			report.PassedQueries++
		}
		report.Queries = append(report.Queries, queryReport)
	}
	report.FailedQueries = report.TotalQueries - report.PassedQueries
	if report.TotalQueries > 0 {
		report.MeanReciprocal = reciprocalSum / float64(report.TotalQueries)
	}
	return report, nil
}

func buildRankingEvalMarkdown(report rankingEvalSnapshotReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Ranking Eval Recap\n\n")
	fmt.Fprintf(&b, "- Generated: `%s`\n", markdownInline(report.GeneratedAt))
	fmt.Fprintf(&b, "- ContextDB version: `%s`\n", markdownInline(report.ContextDBVersion))
	fmt.Fprintf(&b, "- Corpus: `%s`\n", markdownInline(report.Corpus))
	fmt.Fprintf(&b, "- Top K: `%d`\n", report.TopK)
	fmt.Fprintf(&b, "- Total queries: `%d`\n", report.TotalQueries)
	fmt.Fprintf(&b, "- Passed: `%d`\n", report.PassedQueries)
	fmt.Fprintf(&b, "- Failed: `%d`\n", report.FailedQueries)
	fmt.Fprintf(&b, "- Mean reciprocal rank: `%.3f`\n\n", report.MeanReciprocal)

	if report.FailedQueries > 0 {
		fmt.Fprintf(&b, "## Failed Queries\n\n")
		fmt.Fprintf(&b, "| Query | Category | Correct rank | Cutoff | Top result |\n")
		fmt.Fprintf(&b, "| --- | --- | ---: | ---: | --- |\n")
		for _, query := range report.Queries {
			if query.Passed {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %s |\n",
				markdownCell(query.ID),
				markdownCell(query.Category),
				markdownCell(formatRankingEvalRank(query.CorrectRank)),
				query.ExpectedRankCutoff,
				markdownCell(formatRankingTopResult(query)))
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "## Query Results\n\n")
	fmt.Fprintf(&b, "| Query | Category | Passed | Correct rank | Reciprocal rank | Top result | Score | Score breakdown |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | ---: | ---: | --- | ---: | --- |\n")
	for _, query := range report.Queries {
		top := rankingEvalTopResult(query)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %.3f | %s | %.3f | %s |\n",
			markdownCell(query.ID),
			markdownCell(query.Category),
			markdownCell(formatRankingEvalPass(query.Passed)),
			markdownCell(formatRankingEvalRank(query.CorrectRank)),
			query.ReciprocalRank,
			markdownCell(formatRankingTopResult(query)),
			top.Score,
			markdownCell(formatRankingScoreBreakdown(top)))
	}
	return b.String()
}

func readRankingEvalSnapshotReport(path string) (rankingEvalSnapshotReport, error) {
	var report rankingEvalSnapshotReport
	data, err := os.ReadFile(path)
	if err != nil {
		return report, fmt.Errorf("read ranking eval snapshot: %w", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("decode ranking eval snapshot: %w", err)
	}
	return report, nil
}

func buildRankingEvalDiffReport(previous, current rankingEvalSnapshotReport) rankingEvalDiffReport {
	diff := rankingEvalDiffReport{
		SchemaVersion:         1,
		ContextDBVersion:      buildinfo.Version,
		PreviousGeneratedAt:   previous.GeneratedAt,
		CurrentGeneratedAt:    current.GeneratedAt,
		Corpus:                current.Corpus,
		TopK:                  current.TopK,
		TotalQueries:          current.TotalQueries,
		PreviousMRR:           previous.MeanReciprocal,
		CurrentMRR:            current.MeanReciprocal,
		MRRDelta:              current.MeanReciprocal - previous.MeanReciprocal,
		PreviousPassedQueries: previous.PassedQueries,
		CurrentPassedQueries:  current.PassedQueries,
		PassedDelta:           current.PassedQueries - previous.PassedQueries,
	}
	if diff.Corpus == "" {
		diff.Corpus = previous.Corpus
	}
	if diff.TopK == 0 {
		diff.TopK = previous.TopK
	}
	previousByID := rankingEvalQueriesByID(previous.Queries)
	currentByID := rankingEvalQueriesByID(current.Queries)
	for _, query := range current.Queries {
		previousQuery, ok := previousByID[query.ID]
		if !ok {
			diff.MissingPreviousQueries = append(diff.MissingPreviousQueries, query.ID)
			continue
		}
		queryDiff := buildRankingEvalQueryDiff(previousQuery, query)
		diff.Queries = append(diff.Queries, queryDiff)
		diff.ComparedQueries++
		if queryDiff.PreviousPassed != queryDiff.CurrentPassed {
			diff.PassChangedQueries = append(diff.PassChangedQueries, query.ID)
		}
	}
	for _, query := range previous.Queries {
		if _, ok := currentByID[query.ID]; !ok {
			diff.MissingCurrentQueries = append(diff.MissingCurrentQueries, query.ID)
		}
	}
	sort.Strings(diff.MissingPreviousQueries)
	sort.Strings(diff.MissingCurrentQueries)
	sort.Strings(diff.PassChangedQueries)

	diff.LargestRankMovements = append([]rankingEvalDiffQuery(nil), diff.Queries...)
	sort.SliceStable(diff.LargestRankMovements, func(i, j int) bool {
		left := absInt(diff.LargestRankMovements[i].RankDelta)
		right := absInt(diff.LargestRankMovements[j].RankDelta)
		if left == right {
			return diff.LargestRankMovements[i].ID < diff.LargestRankMovements[j].ID
		}
		return left > right
	})
	diff.LargestRankMovements = rankingEvalTopDiffs(diff.LargestRankMovements, 5)

	diff.LargestScoreMovements = append([]rankingEvalDiffQuery(nil), diff.Queries...)
	sort.SliceStable(diff.LargestScoreMovements, func(i, j int) bool {
		left := math.Abs(diff.LargestScoreMovements[i].TopScoreDelta)
		right := math.Abs(diff.LargestScoreMovements[j].TopScoreDelta)
		if left == right {
			return diff.LargestScoreMovements[i].ID < diff.LargestScoreMovements[j].ID
		}
		return left > right
	})
	diff.LargestScoreMovements = rankingEvalTopDiffs(diff.LargestScoreMovements, 5)
	return diff
}

func buildRankingEvalQueryDiff(previous, current rankingEvalSnapshotQuery) rankingEvalDiffQuery {
	previousTop := rankingEvalTopResult(previous)
	currentTop := rankingEvalTopResult(current)
	return rankingEvalDiffQuery{
		ID:                     current.ID,
		Category:               current.Category,
		PreviousPassed:         previous.Passed,
		CurrentPassed:          current.Passed,
		PreviousCorrectRank:    previous.CorrectRank,
		CurrentCorrectRank:     current.CorrectRank,
		RankDelta:              rankingEvalRankDelta(previous.CorrectRank, current.CorrectRank),
		PreviousReciprocalRank: previous.ReciprocalRank,
		CurrentReciprocalRank:  current.ReciprocalRank,
		ReciprocalRankDelta:    current.ReciprocalRank - previous.ReciprocalRank,
		PreviousTopNodeID:      previousTop.NodeID,
		CurrentTopNodeID:       currentTop.NodeID,
		PreviousTopText:        previousTop.Text,
		CurrentTopText:         currentTop.Text,
		PreviousTopScore:       previousTop.Score,
		CurrentTopScore:        currentTop.Score,
		TopScoreDelta:          currentTop.Score - previousTop.Score,
		TopResultChanged:       rankingEvalResultIdentity(previousTop) != rankingEvalResultIdentity(currentTop),
	}
}

func buildRankingEvalDiffMarkdown(diff rankingEvalDiffReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Ranking Eval Diff\n\n")
	fmt.Fprintf(&b, "- Previous: `%s`\n", markdownInline(diff.PreviousGeneratedAt))
	fmt.Fprintf(&b, "- Current: `%s`\n", markdownInline(diff.CurrentGeneratedAt))
	fmt.Fprintf(&b, "- ContextDB version: `%s`\n", markdownInline(diff.ContextDBVersion))
	fmt.Fprintf(&b, "- Corpus: `%s`\n", markdownInline(diff.Corpus))
	fmt.Fprintf(&b, "- Compared queries: `%d`\n", diff.ComparedQueries)
	fmt.Fprintf(&b, "- MRR delta: `%+.3f` (`%.3f` -> `%.3f`)\n", diff.MRRDelta, diff.PreviousMRR, diff.CurrentMRR)
	fmt.Fprintf(&b, "- Passed delta: `%+d` (`%d` -> `%d`)\n\n", diff.PassedDelta, diff.PreviousPassedQueries, diff.CurrentPassedQueries)

	if len(diff.PassChangedQueries) > 0 || len(diff.MissingPreviousQueries) > 0 || len(diff.MissingCurrentQueries) > 0 {
		fmt.Fprintf(&b, "## Attention\n\n")
		if len(diff.PassChangedQueries) > 0 {
			fmt.Fprintf(&b, "- Pass changed: `%s`\n", markdownInline(strings.Join(diff.PassChangedQueries, ", ")))
		}
		if len(diff.MissingPreviousQueries) > 0 {
			fmt.Fprintf(&b, "- Missing in previous snapshot: `%s`\n", markdownInline(strings.Join(diff.MissingPreviousQueries, ", ")))
		}
		if len(diff.MissingCurrentQueries) > 0 {
			fmt.Fprintf(&b, "- Missing in current snapshot: `%s`\n", markdownInline(strings.Join(diff.MissingCurrentQueries, ", ")))
		}
		fmt.Fprintf(&b, "\n")
	}

	writeRankingEvalDiffTable(&b, "Largest Rank Movements", diff.LargestRankMovements)
	writeRankingEvalDiffTable(&b, "Largest Score Movements", diff.LargestScoreMovements)
	return b.String()
}

func writeRankingEvalBaselineArtifacts(dir string, report rankingEvalSnapshotReport) (rankingEvalBaselineArtifacts, error) {
	paths, err := rankingEvalBaselineArtifactPaths(dir, report.ContextDBVersion)
	if err != nil {
		return paths, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return paths, fmt.Errorf("create ranking eval baseline dir: %w", err)
	}
	if err := writeJSONFile(paths.JSONPath, report); err != nil {
		return paths, err
	}
	if err := writeTextFile(paths.MarkdownPath, buildRankingEvalMarkdown(report)); err != nil {
		return paths, err
	}
	return paths, nil
}

func rankingEvalBaselineArtifactPaths(dir, version string) (rankingEvalBaselineArtifacts, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return rankingEvalBaselineArtifacts{}, errors.New("baseline dir is required")
	}
	version = rankingEvalBaselineVersionLabel(version)
	return rankingEvalBaselineArtifacts{
		Version:      version,
		Dir:          dir,
		JSONPath:     filepath.Join(dir, "ranking-eval-"+version+".json"),
		MarkdownPath: filepath.Join(dir, "ranking-eval-"+version+".md"),
	}, nil
}

func buildRankingEvalBaselineRetentionReport(dir string, keep int) (rankingEvalBaselineRetentionReport, error) {
	dir = strings.TrimSpace(dir)
	report := rankingEvalBaselineRetentionReport{
		Dir:  dir,
		Keep: keep,
	}
	if dir == "" {
		report.ValidationErrors = append(report.ValidationErrors, "baseline retention dir is required")
		report.OK = false
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if keep < 0 {
		report.ValidationErrors = append(report.ValidationErrors, "--baseline-retention-keep must be zero or positive")
		report.OK = false
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read ranking eval baseline dir: %v", err))
		report.OK = false
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	type artifactSet struct {
		version  rankingEvalBaselineVersion
		jsonPath string
		mdPath   string
	}
	artifacts := map[string]artifactSet{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "ranking-eval-") {
			continue
		}
		var kind string
		versionLabel := strings.TrimPrefix(name, "ranking-eval-")
		switch {
		case strings.HasSuffix(versionLabel, ".json"):
			kind = "json"
			versionLabel = strings.TrimSuffix(versionLabel, ".json")
		case strings.HasSuffix(versionLabel, ".md"):
			kind = "markdown"
			versionLabel = strings.TrimSuffix(versionLabel, ".md")
		default:
			continue
		}
		version, ok := parseRankingEvalBaselineVersion(versionLabel)
		if !ok {
			continue
		}
		normalized := rankingEvalBaselineVersionString(version)
		set := artifacts[normalized]
		set.version = version
		switch kind {
		case "json":
			set.jsonPath = filepath.Join(dir, name)
		case "markdown":
			set.mdPath = filepath.Join(dir, name)
		}
		artifacts[normalized] = set
	}
	sets := make([]artifactSet, 0, len(artifacts))
	for _, set := range artifacts {
		sets = append(sets, set)
	}
	sort.SliceStable(sets, func(i, j int) bool {
		return compareRankingEvalBaselineVersion(sets[i].version, sets[j].version) > 0
	})
	for i, set := range sets {
		item := rankingEvalBaselineRetentionItem{
			Version:      rankingEvalBaselineVersionString(set.version),
			Current:      i == 0,
			Status:       "pruneable",
			JSONPath:     set.jsonPath,
			MarkdownPath: set.mdPath,
		}
		if i < keep {
			item.Status = "retain"
			report.RetainedVersions++
		} else {
			report.PruneableVersions++
		}
		if item.JSONPath == "" {
			item.Missing = append(item.Missing, "json")
		}
		if item.MarkdownPath == "" {
			item.Missing = append(item.Missing, "markdown")
		}
		report.Baselines = append(report.Baselines, item)
	}
	report.TotalVersions = len(report.Baselines)
	report.DeleteCommands = buildRankingEvalBaselineDeleteCommands(report.Baselines)
	report.OK = true
	return report, nil
}

func buildRankingEvalBaselineArtifactManifest(report rankingEvalBaselineRetentionReport, generatedAt time.Time) (rankingEvalBaselineArtifactManifest, error) {
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	manifest := rankingEvalBaselineArtifactManifest{
		Kind:              "contextdb.ranking.baseline.artifact_manifest",
		SchemaVersion:     1,
		GeneratedAt:       generatedAt.UTC().Format(time.RFC3339),
		ContextDBVersion:  buildinfo.Version,
		Dir:               report.Dir,
		Keep:              report.Keep,
		TotalVersions:     report.TotalVersions,
		RetainedVersions:  report.RetainedVersions,
		PruneableVersions: report.PruneableVersions,
	}
	for _, baseline := range report.Baselines {
		for _, artifact := range []struct {
			kind string
			path string
		}{
			{kind: "json", path: baseline.JSONPath},
			{kind: "markdown", path: baseline.MarkdownPath},
		} {
			item := rankingEvalBaselineArtifactManifestItem{
				Version: baseline.Version,
				Status:  baseline.Status,
				Current: baseline.Current,
				Kind:    artifact.kind,
				Path:    artifact.path,
			}
			if strings.TrimSpace(artifact.path) == "" {
				item.Missing = true
				manifest.Artifacts = append(manifest.Artifacts, item)
				continue
			}
			info, err := os.Stat(artifact.path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					item.Missing = true
					manifest.Artifacts = append(manifest.Artifacts, item)
					continue
				}
				return manifest, fmt.Errorf("stat ranking baseline artifact %s: %w", artifact.path, err)
			}
			if info.IsDir() {
				item.Missing = true
				manifest.Artifacts = append(manifest.Artifacts, item)
				continue
			}
			data, err := os.ReadFile(artifact.path)
			if err != nil {
				return manifest, fmt.Errorf("read ranking baseline artifact %s: %w", artifact.path, err)
			}
			sum := sha256.Sum256(data)
			item.Exists = true
			item.Bytes = int64(len(data))
			item.SHA256 = hex.EncodeToString(sum[:])
			manifest.Artifacts = append(manifest.Artifacts, item)
		}
	}
	return manifest, nil
}

func verifyRankingEvalBaselineArtifactManifest(manifestPath string) (rankingEvalBaselineArtifactManifestVerifyReport, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	report := rankingEvalBaselineArtifactManifestVerifyReport{
		ManifestFile: manifestPath,
	}
	if manifestPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--manifest is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read ranking baseline artifact manifest: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	var manifest rankingEvalBaselineArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode ranking baseline artifact manifest: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.ManifestKind = manifest.Kind
	report.ManifestGeneratedAt = manifest.GeneratedAt
	report.ContextDBVersion = manifest.ContextDBVersion
	report.TotalArtifacts = len(manifest.Artifacts)
	if manifest.Kind != "contextdb.ranking.baseline.artifact_manifest" {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported manifest kind %q", manifest.Kind))
	}
	if manifest.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported manifest schema_version %d", manifest.SchemaVersion))
	}
	for _, artifact := range manifest.Artifacts {
		item := rankingEvalBaselineArtifactManifestVerifyReportItem{
			Version:         artifact.Version,
			Status:          artifact.Status,
			Current:         artifact.Current,
			Kind:            artifact.Kind,
			Path:            artifact.Path,
			ExpectedMissing: artifact.Missing,
			ExpectedBytes:   artifact.Bytes,
			ExpectedSHA256:  artifact.SHA256,
		}
		path := strings.TrimSpace(artifact.Path)
		if artifact.Missing {
			report.MissingArtifacts++
			if path == "" {
				report.ArtifactVerifications = append(report.ArtifactVerifications, item)
				continue
			}
			info, statErr := os.Stat(path)
			switch {
			case statErr == nil && !info.IsDir():
				item.Exists = true
				item.ActualBytes = info.Size()
				msg := "artifact exists but manifest marks it missing"
				item.ValidationErrors = append(item.ValidationErrors, msg)
				report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, msg))
			case statErr == nil && info.IsDir():
				msg := "artifact path is a directory but manifest marks it missing"
				item.ValidationErrors = append(item.ValidationErrors, msg)
				report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, msg))
			case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
				msg := fmt.Sprintf("stat artifact: %v", statErr)
				item.ValidationErrors = append(item.ValidationErrors, msg)
				report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, msg))
			}
			report.ArtifactVerifications = append(report.ArtifactVerifications, item)
			continue
		}
		if path == "" {
			item.ValidationErrors = append(item.ValidationErrors, "artifact path is empty")
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, "artifact path is empty"))
			report.ArtifactVerifications = append(report.ArtifactVerifications, item)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("stat artifact: %v", err))
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, item.ValidationErrors[len(item.ValidationErrors)-1]))
			report.ArtifactVerifications = append(report.ArtifactVerifications, item)
			continue
		}
		if info.IsDir() {
			item.ValidationErrors = append(item.ValidationErrors, "artifact path is a directory")
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, "artifact path is a directory"))
			report.ArtifactVerifications = append(report.ArtifactVerifications, item)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("read artifact: %v", err))
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, item.ValidationErrors[len(item.ValidationErrors)-1]))
			report.ArtifactVerifications = append(report.ArtifactVerifications, item)
			continue
		}
		sum := sha256.Sum256(content)
		item.Exists = true
		item.ActualBytes = int64(len(content))
		item.ActualSHA256 = hex.EncodeToString(sum[:])
		if artifact.Bytes != item.ActualBytes {
			msg := fmt.Sprintf("artifact byte size mismatch: expected %d got %d", artifact.Bytes, item.ActualBytes)
			item.ValidationErrors = append(item.ValidationErrors, msg)
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, msg))
		}
		if !strings.EqualFold(strings.TrimSpace(artifact.SHA256), item.ActualSHA256) {
			msg := "artifact sha256 mismatch"
			item.ValidationErrors = append(item.ValidationErrors, msg)
			report.ValidationErrors = append(report.ValidationErrors, artifactManifestValidationPrefix(item, msg))
		}
		if len(item.ValidationErrors) == 0 {
			report.VerifiedArtifacts++
		}
		report.ArtifactVerifications = append(report.ArtifactVerifications, item)
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("ranking baseline artifact manifest verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func buildRankingEvalBaselineArtifactManifestVerifyMarkdown(report rankingEvalBaselineArtifactManifestVerifyReport) string {
	var b strings.Builder
	b.WriteString("# Ranking Baseline Manifest Verification\n\n")
	status := "passed"
	if !report.OK {
		status = "failed"
	}
	fmt.Fprintf(&b, "- Status: `%s`\n", status)
	fmt.Fprintf(&b, "- Manifest: `%s`\n", markdownInline(report.ManifestFile))
	if strings.TrimSpace(report.ManifestGeneratedAt) != "" {
		fmt.Fprintf(&b, "- Generated at: `%s`\n", markdownInline(report.ManifestGeneratedAt))
	}
	if strings.TrimSpace(report.ContextDBVersion) != "" {
		fmt.Fprintf(&b, "- contextdb: `%s`\n", markdownInline(report.ContextDBVersion))
	}
	fmt.Fprintf(&b, "- Artifacts: `%d` total, `%d` verified, `%d` expected missing\n\n", report.TotalArtifacts, report.VerifiedArtifacts, report.MissingArtifacts)
	if len(report.ValidationErrors) > 0 {
		b.WriteString("## Validation Errors\n\n")
		for _, err := range report.ValidationErrors {
			fmt.Fprintf(&b, "- %s\n", markdownInline(err))
		}
		b.WriteByte('\n')
	}
	b.WriteString("## Artifact Summary\n\n")
	b.WriteString("| Version | Kind | Status | Result | Bytes |\n")
	b.WriteString("|:--------|:-----|:-------|:-------|------:|\n")
	for _, item := range report.ArtifactVerifications {
		result := "verified"
		switch {
		case len(item.ValidationErrors) > 0:
			result = "failed"
		case item.ExpectedMissing:
			result = "expected missing"
		case !item.Exists:
			result = "missing"
		}
		bytes := item.ActualBytes
		if bytes == 0 {
			bytes = item.ExpectedBytes
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %d |\n",
			markdownInline(item.Version),
			markdownInline(item.Kind),
			markdownInline(item.Status),
			markdownInline(result),
			bytes)
	}
	return b.String()
}

func buildRankingEvalBaselineArtifactManifestFailureAnnotations(report rankingEvalBaselineArtifactManifestVerifyReport) string {
	if report.OK && len(report.ValidationErrors) == 0 {
		return ""
	}
	var b strings.Builder
	itemFailures := 0
	for _, item := range report.ArtifactVerifications {
		for _, validationErr := range item.ValidationErrors {
			itemFailures++
			fmt.Fprintf(&b, "::error file=%s,title=%s::%s\n",
				ciAnnotationEscape(item.Path),
				ciAnnotationEscape("Ranking baseline manifest verification"),
				ciAnnotationEscape(artifactManifestValidationPrefix(item, validationErr)))
		}
	}
	if itemFailures == 0 {
		for _, validationErr := range report.ValidationErrors {
			fmt.Fprintf(&b, "::error title=%s::%s\n",
				ciAnnotationEscape("Ranking baseline manifest verification"),
				ciAnnotationEscape(validationErr))
		}
	}
	return b.String()
}

func writeRankingEvalBaselineArtifactManifestVerifyBundle(dir string, report rankingEvalBaselineArtifactManifestVerifyReport) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("--bundle-dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create ranking baseline verification bundle dir: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, "ranking-baseline-manifest-verification.json"), report); err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(dir, "ranking-baseline-manifest-verification.md"), buildRankingEvalBaselineArtifactManifestVerifyMarkdown(report)); err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(dir, "ranking-baseline-manifest-annotations.txt"), buildRankingEvalBaselineArtifactManifestFailureAnnotations(report)); err != nil {
		return err
	}
	if err := writeRankingEvalBaselineArtifactManifestVerifyBundleIndex(dir, report, time.Now().UTC()); err != nil {
		return err
	}
	return nil
}

func writeRankingEvalBaselineArtifactManifestVerifyBundleIndex(dir string, report rankingEvalBaselineArtifactManifestVerifyReport, generatedAt time.Time) error {
	status := "passed"
	if !report.OK {
		status = "failed"
	}
	index := rankingEvalBaselineArtifactManifestVerifyBundleIndex{
		Kind:             "contextdb.ranking.baseline.verification_bundle",
		SchemaVersion:    1,
		GeneratedAt:      generatedAt.Format(time.RFC3339),
		Status:           status,
		OK:               report.OK,
		ManifestFile:     report.ManifestFile,
		ContextDBVersion: report.ContextDBVersion,
	}
	for _, artifact := range []struct {
		kind string
		path string
	}{
		{kind: "json_report", path: filepath.Join(dir, "ranking-baseline-manifest-verification.json")},
		{kind: "markdown_summary", path: filepath.Join(dir, "ranking-baseline-manifest-verification.md")},
		{kind: "ci_annotations", path: filepath.Join(dir, "ranking-baseline-manifest-annotations.txt")},
	} {
		item, err := buildRankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact(artifact.kind, artifact.path)
		if err != nil {
			return err
		}
		index.Artifacts = append(index.Artifacts, item)
	}
	return writeJSONFile(filepath.Join(dir, "ranking-baseline-manifest-verification-index.json"), index)
}

func buildRankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact(kind, path string) (rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact{}, fmt.Errorf("read ranking baseline verification bundle artifact: %w", err)
	}
	sum := sha256.Sum256(data)
	return rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact{
		Kind:   kind,
		Path:   path,
		Bytes:  int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

func verifyRankingEvalBaselineArtifactManifestVerifyBundleIndex(indexPath string) (rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReport, error) {
	indexPath = strings.TrimSpace(indexPath)
	report := rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReport{
		IndexFile: indexPath,
	}
	if indexPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--index is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read ranking baseline verification bundle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	var index rankingEvalBaselineArtifactManifestVerifyBundleIndex
	if err := json.Unmarshal(data, &index); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode ranking baseline verification bundle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.IndexKind = index.Kind
	report.SchemaVersion = index.SchemaVersion
	report.GeneratedAt = index.GeneratedAt
	report.Status = index.Status
	report.BundleOK = index.OK
	report.ManifestFile = index.ManifestFile
	report.ContextDBVersion = index.ContextDBVersion
	report.TotalArtifacts = len(index.Artifacts)
	if index.Kind != "contextdb.ranking.baseline.verification_bundle" {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported bundle index kind %q", index.Kind))
	}
	if index.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported bundle index schema_version %d", index.SchemaVersion))
	}
	expectedStatus := "failed"
	if index.OK {
		expectedStatus = "passed"
	}
	if index.Status != expectedStatus {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("bundle index status %q does not match ok=%t", index.Status, index.OK))
	}

	seenKinds := map[string]bool{}
	var jsonReport *rankingEvalBaselineArtifactManifestVerifyReport
	for _, artifact := range index.Artifacts {
		item := verifyRankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact(indexPath, artifact)
		seenKinds[item.Kind] = true
		if len(item.ValidationErrors) == 0 {
			report.VerifiedArtifacts++
		} else {
			for _, validationErr := range item.ValidationErrors {
				report.ValidationErrors = append(report.ValidationErrors, bundleIndexValidationPrefix(item, validationErr))
			}
		}
		if artifact.Kind == "json_report" && len(item.ValidationErrors) == 0 {
			parsed, parseErr := readRankingEvalBaselineArtifactManifestVerifyBundleJSONReport(item.ResolvedPath)
			if parseErr != nil {
				msg := parseErr.Error()
				item.ValidationErrors = append(item.ValidationErrors, msg)
				report.ValidationErrors = append(report.ValidationErrors, bundleIndexValidationPrefix(item, msg))
				report.VerifiedArtifacts--
			} else {
				jsonReport = &parsed
			}
		}
		report.ArtifactVerifications = append(report.ArtifactVerifications, item)
	}
	for _, requiredKind := range []string{"json_report", "markdown_summary", "ci_annotations"} {
		if !seenKinds[requiredKind] {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("bundle index missing %s artifact", requiredKind))
		}
	}
	if jsonReport != nil {
		if jsonReport.OK != index.OK {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("bundle index ok=%t does not match JSON report ok=%t", index.OK, jsonReport.OK))
		}
		if strings.TrimSpace(index.ManifestFile) != "" && strings.TrimSpace(jsonReport.ManifestFile) != "" && index.ManifestFile != jsonReport.ManifestFile {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("bundle index manifest_file %q does not match JSON report manifest_file %q", index.ManifestFile, jsonReport.ManifestFile))
		}
		if strings.TrimSpace(index.ContextDBVersion) != "" && strings.TrimSpace(jsonReport.ContextDBVersion) != "" && index.ContextDBVersion != jsonReport.ContextDBVersion {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("bundle index contextdb_version %q does not match JSON report contextdb_version %q", index.ContextDBVersion, jsonReport.ContextDBVersion))
		}
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("ranking baseline verification bundle index failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func verifyRankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact(indexPath string, artifact rankingEvalBaselineArtifactManifestVerifyBundleIndexArtifact) rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReportItem {
	item := rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReportItem{
		Kind:           artifact.Kind,
		Path:           artifact.Path,
		ExpectedBytes:  artifact.Bytes,
		ExpectedSHA256: artifact.SHA256,
	}
	if strings.TrimSpace(item.Kind) == "" {
		item.ValidationErrors = append(item.ValidationErrors, "artifact kind is empty")
	}
	path := strings.TrimSpace(artifact.Path)
	if path == "" {
		item.ValidationErrors = append(item.ValidationErrors, "artifact path is empty")
		return item
	}
	resolvedPath := resolveRankingEvalBaselineVerificationBundleArtifactPath(indexPath, path)
	item.ResolvedPath = resolvedPath
	info, err := os.Stat(resolvedPath)
	if err != nil {
		item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("stat artifact: %v", err))
		return item
	}
	if info.IsDir() {
		item.ValidationErrors = append(item.ValidationErrors, "artifact path is a directory")
		return item
	}
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("read artifact: %v", err))
		return item
	}
	sum := sha256.Sum256(content)
	item.Exists = true
	item.ActualBytes = int64(len(content))
	item.ActualSHA256 = hex.EncodeToString(sum[:])
	if artifact.Bytes != item.ActualBytes {
		item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("artifact byte size mismatch: expected %d got %d", artifact.Bytes, item.ActualBytes))
	}
	if !strings.EqualFold(strings.TrimSpace(artifact.SHA256), item.ActualSHA256) {
		item.ValidationErrors = append(item.ValidationErrors, "artifact sha256 mismatch")
	}
	return item
}

func resolveRankingEvalBaselineVerificationBundleArtifactPath(indexPath, artifactPath string) string {
	if filepath.IsAbs(artifactPath) {
		return artifactPath
	}
	if _, err := os.Stat(artifactPath); err == nil {
		return artifactPath
	}
	return filepath.Join(filepath.Dir(indexPath), filepath.Base(artifactPath))
}

func readRankingEvalBaselineArtifactManifestVerifyBundleJSONReport(path string) (rankingEvalBaselineArtifactManifestVerifyReport, error) {
	var report rankingEvalBaselineArtifactManifestVerifyReport
	data, err := os.ReadFile(path)
	if err != nil {
		return report, fmt.Errorf("read JSON report: %w", err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("decode JSON report: %w", err)
	}
	return report, nil
}

func buildRankingEvalBaselineDeleteCommands(baselines []rankingEvalBaselineRetentionItem) []string {
	seen := map[string]bool{}
	var paths []string
	for _, baseline := range baselines {
		if baseline.Status != "pruneable" {
			continue
		}
		for _, path := range []string{baseline.JSONPath, baseline.MarkdownPath} {
			path = strings.TrimSpace(path)
			if path == "" || seen[path] {
				continue
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	sort.Strings(paths)
	commands := make([]string, 0, len(paths))
	for _, path := range paths {
		commands = append(commands, "rm -- "+shellQuote(path))
	}
	return commands
}

func buildRankingEvalBaselineDeleteScript(report rankingEvalBaselineRetentionReport) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n")
	b.WriteString("# Dry-run deletion plan generated by contextdb eval ranking baseline retention.\n")
	b.WriteString("# Review every path before running these commands.\n")
	if len(report.DeleteCommands) == 0 {
		b.WriteString("# No pruneable ranking eval baseline artifacts were found.\n")
		return b.String()
	}
	for _, command := range report.DeleteCommands {
		b.WriteString(command)
		b.WriteByte('\n')
	}
	return b.String()
}

func resolveRankingEvalBaselineComparePath(dir, currentVersion string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("compare baseline dir is required")
	}
	current, ok := parseRankingEvalBaselineVersion(rankingEvalBaselineVersionLabel(currentVersion))
	if !ok {
		return "", fmt.Errorf("current version %q is not a semantic version", currentVersion)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read ranking eval baseline dir: %w", err)
	}
	var candidates []rankingEvalBaselineVersion
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "ranking-eval-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		versionLabel := strings.TrimSuffix(strings.TrimPrefix(name, "ranking-eval-"), ".json")
		version, ok := parseRankingEvalBaselineVersion(versionLabel)
		if !ok || compareRankingEvalBaselineVersion(version, current) >= 0 {
			continue
		}
		version.Path = filepath.Join(dir, name)
		candidates = append(candidates, version)
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no previous ranking eval baseline found in %s", dir)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareRankingEvalBaselineVersion(candidates[i], candidates[j]) > 0
	})
	return candidates[0].Path, nil
}

func rankingEvalBaselineVersionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = buildinfo.Version
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func parseRankingEvalBaselineVersion(version string) (rankingEvalBaselineVersion, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return rankingEvalBaselineVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return rankingEvalBaselineVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return rankingEvalBaselineVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return rankingEvalBaselineVersion{}, false
	}
	return rankingEvalBaselineVersion{Major: major, Minor: minor, Patch: patch}, true
}

func rankingEvalBaselineVersionString(version rankingEvalBaselineVersion) string {
	return fmt.Sprintf("v%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func compareRankingEvalBaselineVersion(left, right rankingEvalBaselineVersion) int {
	if left.Major != right.Major {
		return left.Major - right.Major
	}
	if left.Minor != right.Minor {
		return left.Minor - right.Minor
	}
	return left.Patch - right.Patch
}

func writeRankingEvalDiffTable(b *strings.Builder, title string, queries []rankingEvalDiffQuery) {
	fmt.Fprintf(b, "## %s\n\n", title)
	fmt.Fprintf(b, "| Query | Category | Rank | Rank delta | Top score | Score delta | Top changed |\n")
	fmt.Fprintf(b, "| --- | --- | --- | ---: | --- | ---: | --- |\n")
	for _, query := range queries {
		fmt.Fprintf(b, "| %s | %s | %s -> %s | %+d | %.3f -> %.3f | %+.3f | %s |\n",
			markdownCell(query.ID),
			markdownCell(query.Category),
			markdownCell(formatRankingEvalRank(query.PreviousCorrectRank)),
			markdownCell(formatRankingEvalRank(query.CurrentCorrectRank)),
			query.RankDelta,
			query.PreviousTopScore,
			query.CurrentTopScore,
			query.TopScoreDelta,
			markdownCell(formatRankingEvalPass(query.TopResultChanged)))
	}
	fmt.Fprintf(b, "\n")
}

func rankingEvalQueriesByID(queries []rankingEvalSnapshotQuery) map[string]rankingEvalSnapshotQuery {
	byID := make(map[string]rankingEvalSnapshotQuery, len(queries))
	for _, query := range queries {
		byID[query.ID] = query
	}
	return byID
}

func rankingEvalRankDelta(previousRank, currentRank int) int {
	if previousRank == 0 || currentRank == 0 {
		return currentRank - previousRank
	}
	return previousRank - currentRank
}

func rankingEvalTopDiffs(queries []rankingEvalDiffQuery, limit int) []rankingEvalDiffQuery {
	if len(queries) <= limit {
		return queries
	}
	return queries[:limit]
}

func rankingEvalResultIdentity(result rankingEvalSnapshotResult) string {
	if strings.TrimSpace(result.Text) != "" {
		return strings.TrimSpace(result.Text)
	}
	return result.NodeID
}

func rankingEvalTopResult(query rankingEvalSnapshotQuery) rankingEvalSnapshotResult {
	if len(query.TopResults) == 0 {
		return rankingEvalSnapshotResult{}
	}
	return query.TopResults[0]
}

func formatRankingTopResult(query rankingEvalSnapshotQuery) string {
	top := rankingEvalTopResult(query)
	if top.NodeID == "" {
		return "none"
	}
	expected := ""
	if top.Expected {
		expected = " expected"
	}
	return fmt.Sprintf("#%d %s%s", top.Rank, top.NodeID, expected)
}

func formatRankingScoreBreakdown(result rankingEvalSnapshotResult) string {
	if result.NodeID == "" {
		return "none"
	}
	breakdown := result.ScoreBreakdown
	return fmt.Sprintf("sim %.3f, conf %.3f, rec %.3f, util %.3f",
		breakdown.Similarity,
		breakdown.Confidence,
		breakdown.Recency,
		breakdown.Utility)
}

func formatRankingEvalRank(rank int) string {
	if rank == 0 {
		return "missing"
	}
	return strconv.Itoa(rank)
}

func formatRankingEvalPass(passed bool) string {
	if passed {
		return "yes"
	}
	return "no"
}

func rankingEvalExpectedRankCutoff(category string) int {
	switch category {
	case "poisoning", "temporal", "procedural":
		return 1
	default:
		return 3
	}
}

func rankingEvalCorpusMode(ns string) namespace.Mode {
	switch ns {
	case testdata.NSChannel:
		return namespace.ModeBeliefSystem
	case testdata.NSAgent:
		return namespace.ModeAgentMemory
	case testdata.NSProcedural:
		return namespace.ModeProcedural
	default:
		return namespace.ModeGeneral
	}
}

func rankingEvalContainsNode(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
