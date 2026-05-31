// Command contextdb starts the ContextDB server.
//
// Configuration is via environment variables:
//
//	CONTEXTDB_MODE                    embedded | standard | remote (default: embedded)
//	CONTEXTDB_DATA_DIR                data directory for embedded+badger (empty = in-memory)
//	CONTEXTDB_DSN                     Postgres DSN for standard mode
//	CONTEXTDB_GRPC_ADDR               gRPC listen address  (default: :7700)
//	CONTEXTDB_REST_ADDR               REST listen address   (default: :7701)
//	CONTEXTDB_OBS_ADDR                observe listen address (default: :7702)
//	CONTEXTDB_LOG_LEVEL               debug | info | warn | error (default: info)
//	CONTEXTDB_FEDERATION_ENABLED      true | false (default: false)
//	CONTEXTDB_FEDERATION_BIND_ADDR    memberlist bind address (default: :7710)
//	CONTEXTDB_FEDERATION_SEED_PEERS   comma-separated list of seed peer addresses
//	CONTEXTDB_FEDERATION_NAMESPACES   comma-separated list of namespaces to federate (empty = all)
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/doctor"
	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
	"github.com/antiartificial/contextdb/testdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctor(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "norn" {
		runNorn(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "snapshot" {
		runSnapshot(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "eval" {
		runEval(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "repair" {
		runRepair(os.Args[2:])
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(getenv("CONTEXTDB_LOG_LEVEL", "info")),
	}))
	slog.SetDefault(logger)

	// Open client DB
	opts := client.Options{
		Mode:        client.Mode(getenv("CONTEXTDB_MODE", "embedded")),
		DataDir:     os.Getenv("CONTEXTDB_DATA_DIR"),
		DSN:         os.Getenv("CONTEXTDB_DSN"),
		DedupWrites: os.Getenv("CONTEXTDB_DEDUP_WRITES") == "true",
		Logger:      logger,
	}

	db, err := client.Open(opts)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	cfg := server.Config{
		GRPCAddr:    getenv("CONTEXTDB_GRPC_ADDR", ":7700"),
		RESTAddr:    getenv("CONTEXTDB_REST_ADDR", ":7701"),
		ObserveAddr: getenv("CONTEXTDB_OBS_ADDR", ":7702"),
		Federation: federation.Config{
			Enabled:    os.Getenv("CONTEXTDB_FEDERATION_ENABLED") == "true",
			BindAddr:   getenv("CONTEXTDB_FEDERATION_BIND_ADDR", ":7710"),
			SeedPeers:  splitComma(os.Getenv("CONTEXTDB_FEDERATION_SEED_PEERS")),
			Namespaces: splitComma(os.Getenv("CONTEXTDB_FEDERATION_NAMESPACES")),
		},
	}

	srv := server.New(db, db.Registry(), cfg, logger)
	if err := srv.Start(); err != nil {
		logger.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	logger.Info("contextdb started",
		"mode", opts.Mode,
		"grpc", cfg.GRPCAddr,
		"rest", cfg.RESTAddr,
		"observe", cfg.ObserveAddr,
	)

	// Wait for shutdown signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logger.Info("shutting down...")
	srv.Stop()
}

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
	default:
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runEvalRankingBaselineManifestVerify(args []string) {
	fs := flag.NewFlagSet("contextdb eval ranking baseline manifest verify", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "JSON ranking baseline artifact manifest to verify")
	reportOut := fs.Bool("report", false, "print a JSON ranking baseline manifest verification report")
	_ = fs.Parse(args)

	report, err := verifyRankingEvalBaselineArtifactManifest(*manifestPath)
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb eval ranking baseline manifest verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runRepair(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb repair: expected vector-index or kv-cache")
		os.Exit(2)
	}
	switch args[0] {
	case "vector-index":
		runRepairVectorIndex(args[1:])
	case "kv-cache":
		runRepairKVCache(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb repair: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runRepairVectorIndex(args []string) {
	fs := flag.NewFlagSet("contextdb repair vector-index", flag.ExitOnError)
	namespace := fs.String("namespace", "default", "namespace to scan for vector index repair candidates")
	sampleLimit := fs.Int("sample", 100, "maximum valid graph nodes to scan")
	execute := fs.Bool("execute", false, "write rebuilt vector index entries for candidates")
	reportOut := fs.Bool("report", false, "print the JSON repair report")
	_ = fs.Parse(args)

	db := openSnapshotDB()
	defer db.Close()
	graph, vecs, _, _ := db.Stores()
	report, err := buildVectorIndexRepairReport(context.Background(), graph, vecs, *namespace, *sampleLimit, *execute)
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb repair vector-index: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runRepairKVCache(args []string) {
	fs := flag.NewFlagSet("contextdb repair kv-cache", flag.ExitOnError)
	var keys repeatedStringFlag
	fs.Var(&keys, "key", "KV hot key to refresh; repeat for multiple keys")
	value := fs.String("value", "", "literal value to write for each refresh candidate")
	valueFile := fs.String("value-file", "", "file containing the value to write for each refresh candidate")
	derive := fs.String("derive", "", "derive a reviewed refresh value; supported: recent-nodes")
	deriveNamespace := fs.String("derive-namespace", "default", "namespace to read when deriving a refresh value")
	var deriveLabels repeatedStringFlag
	fs.Var(&deriveLabels, "derive-label", "label filter for derived refresh values; repeat for multiple labels")
	deriveLimit := fs.Int("derive-limit", 5, "maximum nodes to include in derived refresh values")
	ttl := fs.Int("ttl", 0, "TTL seconds for refreshed keys; 0 means no explicit expiry")
	overwrite := fs.Bool("overwrite", false, "refresh keys even when they already have values")
	execute := fs.Bool("execute", false, "write reviewed KV refresh candidates")
	reportOut := fs.Bool("report", false, "print the JSON KV refresh report")
	_ = fs.Parse(args)

	db := openSnapshotDB()
	defer db.Close()
	graph, _, kv, _ := db.Stores()
	valueBytes, valueSource, err := kvRefreshValue(context.Background(), graph, kvRefreshValueOptions{
		Value:           *value,
		ValueFile:       *valueFile,
		Derive:          *derive,
		DeriveNamespace: *deriveNamespace,
		DeriveLabels:    deriveLabels,
		DeriveLimit:     *deriveLimit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb repair kv-cache: %v\n", err)
		os.Exit(2)
	}
	report, err := buildKVRefreshReport(context.Background(), kv, kvRefreshOptions{
		Keys:        keys,
		Value:       valueBytes,
		ValueSource: valueSource,
		TTLSeconds:  *ttl,
		Overwrite:   *overwrite,
		Execute:     *execute,
		GeneratedAt: time.Now(),
	})
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb repair kv-cache: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshot(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb snapshot: expected export, import, verify, rehearse, receipt, or lifecycle")
		os.Exit(2)
	}
	switch args[0] {
	case "export":
		runSnapshotExport(args[1:])
	case "import":
		runSnapshotImport(args[1:])
	case "verify":
		runSnapshotVerify(args[1:])
	case "rehearse":
		runSnapshotRehearse(args[1:])
	case "receipt":
		runSnapshotReceipt(args[1:])
	case "lifecycle":
		runSnapshotLifecycle(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb snapshot: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runSnapshotExport(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot export", flag.ExitOnError)
	namespace := fs.String("namespace", "default", "namespace to export")
	outPath := fs.String("out", "-", "output NDJSON file, or - for stdout")
	seedRaw := fs.String("seeds", "", "comma-separated seed node IDs for filtered export")
	maxDepth := fs.Int("max-depth", 10, "maximum graph depth for seeded exports")
	backupMarker := fs.String("backup-marker", "", "marker file to write after successful export")
	manifestPath := fs.String("manifest", "", "JSON artifact manifest to write after successful export")
	_ = fs.Parse(args)

	db := openSnapshotDB()
	defer db.Close()

	out, closeOut, err := outputWriter(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot export: %v\n", err)
		os.Exit(2)
	}
	defer closeOut()

	seeds, err := parseUUIDList(*seedRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot export: %v\n", err)
		os.Exit(2)
	}
	if len(seeds) > 0 {
		err = db.ExportSnapshotFromSeeds(context.Background(), *namespace, seeds, *maxDepth, out)
	} else {
		err = db.ExportSnapshot(context.Background(), *namespace, out)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot export: %v\n", err)
		os.Exit(1)
	}
	exportedAt := time.Now()
	if err := writeBackupMarker(*backupMarker, exportedAt); err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot export: %v\n", err)
		os.Exit(1)
	}
	if err := writeSnapshotArtifactManifest(*manifestPath, snapshotArtifactManifestOptions{
		Namespace:    *namespace,
		BackupPath:   *outPath,
		BackupMarker: *backupMarker,
		CreatedAt:    exportedAt,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot export: %v\n", err)
		os.Exit(1)
	}
}

func runSnapshotImport(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot import", flag.ExitOnError)
	namespace := fs.String("namespace", "default", "namespace to import into")
	inPath := fs.String("in", "-", "input NDJSON file, or - for stdin")
	dryRun := fs.Bool("dry-run", false, "validate the snapshot without writing")
	reportOut := fs.Bool("report", false, "print a JSON import report")
	promotionNote := fs.String("promotion-note", "", "operator note to include in the promotion receipt")
	promotionReport := fs.String("promotion-report", "", "JSON promotion receipt to write after successful import")
	_ = fs.Parse(args)
	if *dryRun && strings.TrimSpace(*promotionReport) != "" {
		fmt.Fprintln(os.Stderr, "contextdb snapshot import: --promotion-report requires a real import, not --dry-run")
		os.Exit(2)
	}

	in, closeIn, err := inputReader(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot import: %v\n", err)
		os.Exit(2)
	}
	defer closeIn()

	db := openSnapshotDB()
	defer db.Close()
	var report client.SnapshotReport
	if *dryRun {
		report, err = db.ValidateSnapshotReport(context.Background(), *namespace, in)
	} else {
		report, err = db.ImportSnapshotReport(context.Background(), *namespace, in)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot import: %v\n", err)
		os.Exit(1)
	}
	if !*dryRun {
		if err := writeSnapshotPromotionReceipt(*promotionReport, snapshotPromotionReceiptOptions{
			Namespace:  *namespace,
			BackupPath: *inPath,
			Note:       *promotionNote,
			ImportedAt: time.Now(),
			Report:     report,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "contextdb snapshot import: %v\n", err)
			os.Exit(1)
		}
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if *dryRun {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotVerify(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot verify", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "JSON artifact manifest to verify")
	inPath := fs.String("in", "", "input NDJSON file, defaults to manifest backup_file beside manifest")
	reportOut := fs.Bool("report", false, "print a JSON verification report")
	_ = fs.Parse(args)

	report, err := verifySnapshotArtifactManifest(*manifestPath, *inPath)
	if err != nil {
		if *reportOut && (report.Manifest != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot verify: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotRehearse(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot rehearse", flag.ExitOnError)
	namespace := fs.String("namespace", "restore-preview", "namespace to dry-run the import into")
	manifestPath := fs.String("manifest", "", "JSON artifact manifest to verify")
	inPath := fs.String("in", "", "input NDJSON file, defaults to manifest backup_file beside manifest")
	reportOut := fs.Bool("report", false, "print a JSON rehearsal report")
	_ = fs.Parse(args)

	db := openSnapshotDB()
	defer db.Close()
	report, err := rehearseSnapshotRestore(context.Background(), db, *namespace, *manifestPath, *inPath)
	if err != nil {
		if *reportOut && (report.Verification.Manifest != "" || len(report.Verification.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot rehearse: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotReceipt(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb snapshot receipt: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runSnapshotReceiptVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb snapshot receipt: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runSnapshotReceiptVerify(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot receipt verify", flag.ExitOnError)
	promotionReport := fs.String("promotion-report", "", "JSON promotion receipt to verify")
	manifestPath := fs.String("manifest", "", "JSON artifact manifest to compare against")
	reportOut := fs.Bool("report", false, "print a JSON receipt verification report")
	_ = fs.Parse(args)

	report, err := verifySnapshotPromotionReceipt(*promotionReport, *manifestPath)
	if err != nil {
		if *reportOut && (report.PromotionReport != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot receipt verify: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotLifecycle(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb snapshot lifecycle: expected verify, retention, or index")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runSnapshotLifecycleVerify(args[1:])
	case "retention":
		runSnapshotLifecycleRetention(args[1:])
	case "index":
		runSnapshotLifecycleIndex(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runSnapshotLifecycleVerify(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle verify", flag.ExitOnError)
	summaryPath := fs.String("summary", "", "JSON lifecycle summary to verify")
	reportOut := fs.Bool("report", false, "print a JSON lifecycle verification report")
	_ = fs.Parse(args)

	report, err := verifySnapshotLifecycleSummary(*summaryPath)
	if err != nil {
		if *reportOut && (report.Summary != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle verify: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotLifecycleIndex(args []string) {
	if len(args) > 0 && args[0] == "diff" {
		runSnapshotLifecycleIndexDiff(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "publish" {
		runSnapshotLifecycleIndexPublish(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "verify" {
		runSnapshotLifecycleIndexVerify(args[1:])
		return
	}
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index", flag.ExitOnError)
	dir := fs.String("dir", "", "directory containing lifecycle summary files")
	namespace := fs.String("namespace", "", "optional namespace filter")
	keep := fs.Int("keep", 14, "number of newest lifecycle bundles to mark as kept")
	outPath := fs.String("out", "", "JSON index file to write, defaults to contextdb-backups.index.json in --dir")
	reportOut := fs.Bool("report", false, "print the JSON lifecycle index")
	_ = fs.Parse(args)

	index, err := writeSnapshotLifecycleIndex(*outPath, snapshotLifecycleIndexOptions{
		Dir:       *dir,
		Namespace: *namespace,
		Keep:      *keep,
		CreatedAt: time.Now(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(index)
	} else {
		fmt.Fprintln(os.Stdout, index.IndexFile)
	}
}

func runSnapshotLifecycleIndexPublish(args []string) {
	if len(args) > 0 && args[0] == "receipt" {
		runSnapshotLifecycleIndexPublishReceipt(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "drift" {
		runSnapshotLifecycleIndexPublishDrift(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "freshness" {
		runSnapshotLifecycleIndexPublishFreshness(args[1:])
		return
	}
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index publish", flag.ExitOnError)
	inPath := fs.String("in", "", "JSON lifecycle index to publish")
	publishURL := fs.String("publish-url", os.Getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL"), "backup index metadata publish endpoint")
	method := fs.String("method", getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_METHOD", http.MethodPost), "HTTP method for publishing")
	token := fs.String("token", os.Getenv("NORN_TOKEN"), "optional bearer token for the publish endpoint")
	dryRunFlag := fs.Bool("dry-run", true, "validate and print the publish plan without sending it")
	execute := fs.Bool("execute", false, "send the backup index metadata to --publish-url")
	receiptOut := fs.String("receipt-out", "", "write a JSON publish receipt after successful --execute")
	reportOut := fs.Bool("report", false, "print a JSON backup index publish report")
	timeout := fs.Duration("timeout", 5*time.Second, "publish request timeout")
	_ = fs.Parse(args)

	dryRun := *dryRunFlag && !*execute
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := buildSnapshotLifecycleIndexPublishReport(ctx, http.DefaultClient, *inPath, snapshotLifecycleIndexPublishOptions{
		PublishURL: *publishURL,
		Method:     *method,
		Token:      *token,
		DryRun:     dryRun,
		ReceiptOut: *receiptOut,
	})
	if err != nil {
		if *reportOut && (report.IndexFile != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index publish: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if report.DryRun {
		fmt.Fprintln(os.Stdout, "dry-run ok")
	} else {
		fmt.Fprintln(os.Stdout, "published")
	}
}

func runSnapshotLifecycleIndexPublishReceipt(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb snapshot lifecycle index publish receipt: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runSnapshotLifecycleIndexPublishReceiptVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index publish receipt: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runSnapshotLifecycleIndexPublishReceiptVerify(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index publish receipt verify", flag.ExitOnError)
	receiptPath := fs.String("receipt", "", "JSON lifecycle index publish receipt to verify")
	inPath := fs.String("in", "", "JSON lifecycle index to compare against the receipt")
	reportOut := fs.Bool("report", false, "print a JSON lifecycle index publish receipt verification report")
	_ = fs.Parse(args)

	report, err := verifySnapshotLifecycleIndexPublishReceipt(*receiptPath, *inPath)
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index publish receipt verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotLifecycleIndexPublishDrift(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index publish drift", flag.ExitOnError)
	inPath := fs.String("in", "", "local JSON lifecycle index to compare")
	publishedURL := fs.String("published-url", os.Getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL"), "published backup index metadata URL")
	method := fs.String("method", getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_METHOD", http.MethodGet), "HTTP method for fetching published metadata")
	token := fs.String("token", os.Getenv("NORN_TOKEN"), "optional bearer token for the published metadata endpoint")
	reportOut := fs.Bool("report", false, "print a JSON backup index publish drift report")
	timeout := fs.Duration("timeout", 5*time.Second, "published metadata request timeout")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := buildSnapshotLifecycleIndexPublishDriftReport(ctx, http.DefaultClient, *inPath, snapshotLifecycleIndexPublishDriftOptions{
		PublishedURL: *publishedURL,
		Method:       *method,
		Token:        *token,
	})
	if err != nil {
		if *reportOut && (report.IndexFile != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index publish drift: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if report.Drift {
		fmt.Fprintln(os.Stdout, "drift detected")
	} else {
		fmt.Fprintln(os.Stdout, "no drift")
	}
}

func runSnapshotLifecycleIndexPublishFreshness(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index publish freshness", flag.ExitOnError)
	publishedURL := fs.String("published-url", os.Getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL"), "published backup index metadata URL")
	method := fs.String("method", getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_METHOD", http.MethodGet), "HTTP method for fetching published metadata")
	token := fs.String("token", os.Getenv("NORN_TOKEN"), "optional bearer token for the published metadata endpoint")
	maxAge := fs.Duration("max-age", 24*time.Hour, "maximum acceptable age for published generated_at")
	reportOut := fs.Bool("report", false, "print a JSON backup index publish freshness report")
	timeout := fs.Duration("timeout", 5*time.Second, "published metadata request timeout")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := buildSnapshotLifecycleIndexPublishFreshnessReport(ctx, http.DefaultClient, snapshotLifecycleIndexPublishFreshnessOptions{
		PublishedURL: *publishedURL,
		Method:       *method,
		Token:        *token,
		MaxAge:       *maxAge,
		Now:          time.Now(),
	})
	if err != nil {
		if *reportOut && (report.PublishedURL != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index publish freshness: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if report.Fresh {
		fmt.Fprintln(os.Stdout, "fresh")
	} else {
		fmt.Fprintln(os.Stdout, "stale")
	}
}

func runSnapshotLifecycleIndexDiff(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index diff", flag.ExitOnError)
	oldPath := fs.String("old", "", "previous JSON lifecycle index to compare")
	newPath := fs.String("new", "", "new JSON lifecycle index to compare")
	reportOut := fs.Bool("report", false, "print a JSON lifecycle index diff report")
	_ = fs.Parse(args)

	report, err := diffSnapshotLifecycleIndexes(*oldPath, *newPath)
	if err != nil {
		if *reportOut && (report.OldIndex != "" || report.NewIndex != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index diff: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotLifecycleIndexVerify(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle index verify", flag.ExitOnError)
	inPath := fs.String("in", "", "JSON lifecycle index to verify")
	reportOut := fs.Bool("report", false, "print a JSON lifecycle index verification report")
	_ = fs.Parse(args)

	report, err := verifySnapshotLifecycleIndex(*inPath)
	if err != nil {
		if *reportOut && (report.IndexFile != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle index verify: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runSnapshotLifecycleRetention(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle retention", flag.ExitOnError)
	dir := fs.String("dir", "", "directory containing lifecycle summary files")
	namespace := fs.String("namespace", "", "optional namespace filter")
	keep := fs.Int("keep", 14, "number of newest lifecycle bundles to keep")
	reportOut := fs.Bool("report", false, "print a JSON retention report")
	emitDeleteScript := fs.Bool("emit-delete-script", false, "print a shell script for pruneable artifacts without deleting files")
	_ = fs.Parse(args)

	report, err := buildSnapshotLifecycleRetentionReport(*dir, *namespace, *keep)
	if err != nil {
		if *reportOut && (report.Dir != "" || len(report.ValidationErrors) > 0) {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb snapshot lifecycle retention: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if *emitDeleteScript {
		fmt.Print(buildSnapshotLifecycleDeleteScript(report))
	} else {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func openSnapshotDB() *client.DB {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := client.Open(client.Options{
		Mode:        client.Mode(getenv("CONTEXTDB_MODE", "embedded")),
		DataDir:     os.Getenv("CONTEXTDB_DATA_DIR"),
		DSN:         os.Getenv("CONTEXTDB_DSN"),
		DedupWrites: os.Getenv("CONTEXTDB_DEDUP_WRITES") == "true",
		Logger:      logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb snapshot: open database: %v\n", err)
		os.Exit(2)
	}
	return db
}

func outputWriter(path string) (io.Writer, func(), error) {
	if path == "-" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("create output: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func inputReader(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open input: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func parseUUIDList(raw string) ([]uuid.UUID, error) {
	parts := splitComma(raw)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, fmt.Errorf("invalid seed %q: %w", part, err)
		}
		out = append(out, id)
	}
	return out, nil
}

func writeBackupMarker(path string, at time.Time) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(at.UTC().Format(time.RFC3339)+"\n"), 0o644)
}

type snapshotArtifactManifestOptions struct {
	Namespace    string
	BackupPath   string
	BackupMarker string
	CreatedAt    time.Time
}

type snapshotArtifactManifest struct {
	SchemaVersion    int                    `json:"schema_version"`
	Namespace        string                 `json:"namespace"`
	BackupFile       string                 `json:"backup_file"`
	BackupBytes      int64                  `json:"backup_bytes"`
	ChecksumSHA256   string                 `json:"checksum_sha256"`
	CreatedAt        string                 `json:"created_at"`
	ContextDBVersion string                 `json:"contextdb_version"`
	BackupMarker     string                 `json:"backup_marker,omitempty"`
	Records          snapshotArtifactCounts `json:"records"`
}

type snapshotArtifactCounts struct {
	Lines   int `json:"lines"`
	Nodes   int `json:"nodes"`
	Edges   int `json:"edges"`
	Sources int `json:"sources"`
}

type snapshotArtifactVerifyReport struct {
	OK               bool                   `json:"ok"`
	Manifest         string                 `json:"manifest"`
	BackupFile       string                 `json:"backup_file"`
	ExpectedBytes    int64                  `json:"expected_bytes"`
	ActualBytes      int64                  `json:"actual_bytes"`
	ExpectedSHA256   string                 `json:"expected_sha256"`
	ActualSHA256     string                 `json:"actual_sha256"`
	ExpectedRecords  snapshotArtifactCounts `json:"expected_records"`
	ActualRecords    snapshotArtifactCounts `json:"actual_records"`
	ContextDBVersion string                 `json:"contextdb_version"`
	ManifestVersion  string                 `json:"manifest_contextdb_version"`
	SchemaVersion    int                    `json:"schema_version"`
	ValidationErrors []string               `json:"validation_errors,omitempty"`
}

type snapshotRehearsalReport struct {
	OK                       bool                         `json:"ok"`
	Namespace                string                       `json:"namespace"`
	RehearsedAt              string                       `json:"rehearsed_at"`
	TargetNamespace          string                       `json:"target_namespace"`
	RecommendedImportCommand string                       `json:"recommended_import_command"`
	Verification             snapshotArtifactVerifyReport `json:"verification"`
	Restore                  client.SnapshotReport        `json:"restore"`
}

type snapshotPromotionReceiptOptions struct {
	Namespace  string
	BackupPath string
	Note       string
	ImportedAt time.Time
	Report     client.SnapshotReport
}

type snapshotPromotionReceipt struct {
	SchemaVersion    int                   `json:"schema_version"`
	Namespace        string                `json:"namespace"`
	BackupFile       string                `json:"backup_file"`
	PromotedAt       string                `json:"promoted_at"`
	ContextDBVersion string                `json:"contextdb_version"`
	PromotionNote    string                `json:"promotion_note,omitempty"`
	ImportReport     client.SnapshotReport `json:"import_report"`
}

type snapshotPromotionReceiptVerifyReport struct {
	OK                 bool                   `json:"ok"`
	PromotionReport    string                 `json:"promotion_report"`
	Manifest           string                 `json:"manifest"`
	ReceiptNamespace   string                 `json:"receipt_namespace"`
	ImportNamespace    string                 `json:"import_namespace"`
	ReceiptBackupFile  string                 `json:"receipt_backup_file"`
	ManifestBackupFile string                 `json:"manifest_backup_file"`
	ReceiptVersion     string                 `json:"receipt_contextdb_version"`
	ManifestVersion    string                 `json:"manifest_contextdb_version"`
	ImportedRecords    snapshotArtifactCounts `json:"imported_records"`
	ManifestRecords    snapshotArtifactCounts `json:"manifest_records"`
	PromotedAt         string                 `json:"promoted_at"`
	ValidationErrors   []string               `json:"validation_errors,omitempty"`
}

type snapshotLifecycleSummary struct {
	Namespace    string `json:"namespace"`
	CreatedAt    string `json:"created_at"`
	Backup       string `json:"backup"`
	Manifest     string `json:"manifest"`
	Rehearsal    string `json:"rehearsal"`
	Promotion    string `json:"promotion"`
	ReceiptCheck string `json:"receipt_check"`
	Promoted     bool   `json:"promoted"`
}

type snapshotLifecycleVerifyReport struct {
	OK               bool     `json:"ok"`
	Summary          string   `json:"summary"`
	Namespace        string   `json:"namespace"`
	CreatedAt        string   `json:"created_at"`
	Promoted         bool     `json:"promoted"`
	Backup           string   `json:"backup"`
	BackupExists     bool     `json:"backup_exists"`
	Manifest         string   `json:"manifest"`
	ManifestExists   bool     `json:"manifest_exists"`
	ManifestOK       bool     `json:"manifest_ok"`
	Rehearsal        string   `json:"rehearsal"`
	RehearsalExists  bool     `json:"rehearsal_exists"`
	RehearsalOK      bool     `json:"rehearsal_ok"`
	Promotion        string   `json:"promotion,omitempty"`
	PromotionExists  bool     `json:"promotion_exists"`
	PromotionOK      bool     `json:"promotion_ok"`
	ReceiptCheck     string   `json:"receipt_check,omitempty"`
	ReceiptCheckOK   bool     `json:"receipt_check_ok"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
}

type snapshotLifecycleRetentionReport struct {
	OK               bool                               `json:"ok"`
	Dir              string                             `json:"dir"`
	Namespace        string                             `json:"namespace,omitempty"`
	Keep             int                                `json:"keep"`
	TotalBundles     int                                `json:"total_bundles"`
	KeepBundles      int                                `json:"keep_bundles"`
	PruneableBundles int                                `json:"pruneable_bundles"`
	DeleteCommands   []string                           `json:"delete_commands,omitempty"`
	Bundles          []snapshotLifecycleRetentionBundle `json:"bundles"`
	ValidationErrors []string                           `json:"validation_errors,omitempty"`
}

type snapshotLifecycleRetentionBundle struct {
	Namespace string                               `json:"namespace"`
	CreatedAt string                               `json:"created_at"`
	Summary   string                               `json:"summary"`
	Promoted  bool                                 `json:"promoted"`
	Decision  string                               `json:"decision"`
	Reason    string                               `json:"reason"`
	Artifacts []snapshotLifecycleRetentionArtifact `json:"artifacts"`
	sortTime  time.Time
}

type snapshotLifecycleRetentionArtifact struct {
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	Bytes          int64  `json:"bytes,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
}

type snapshotLifecycleIndexOptions struct {
	Dir       string
	Namespace string
	Keep      int
	CreatedAt time.Time
}

type snapshotLifecycleIndexPublishOptions struct {
	PublishURL string
	Method     string
	Token      string
	DryRun     bool
	ReceiptOut string
}

type snapshotLifecycleIndexPublishDriftOptions struct {
	PublishedURL string
	Method       string
	Token        string
}

type snapshotLifecycleIndexPublishFreshnessOptions struct {
	PublishedURL string
	Method       string
	Token        string
	MaxAge       time.Duration
	Now          time.Time
}

type snapshotLifecycleIndex struct {
	SchemaVersion    int                                `json:"schema_version"`
	IndexFile        string                             `json:"index_file"`
	GeneratedAt      string                             `json:"generated_at"`
	ContextDBVersion string                             `json:"contextdb_version"`
	Dir              string                             `json:"dir"`
	Namespace        string                             `json:"namespace,omitempty"`
	Keep             int                                `json:"keep"`
	TotalBundles     int                                `json:"total_bundles"`
	KeepBundles      int                                `json:"keep_bundles"`
	PruneableBundles int                                `json:"pruneable_bundles"`
	DeleteCommands   []string                           `json:"delete_commands,omitempty"`
	Bundles          []snapshotLifecycleRetentionBundle `json:"bundles"`
}

type snapshotLifecycleIndexVerifyReport struct {
	OK                bool                                  `json:"ok"`
	IndexFile         string                                `json:"index_file"`
	SchemaVersion     int                                   `json:"schema_version"`
	ContextDBVersion  string                                `json:"contextdb_version"`
	TotalBundles      int                                   `json:"total_bundles"`
	TotalArtifacts    int                                   `json:"total_artifacts"`
	VerifiedArtifacts int                                   `json:"verified_artifacts"`
	ValidationErrors  []string                              `json:"validation_errors,omitempty"`
	Artifacts         []snapshotLifecycleIndexArtifactCheck `json:"artifacts"`
}

type snapshotLifecycleIndexArtifactCheck struct {
	Kind           string   `json:"kind"`
	Path           string   `json:"path"`
	Exists         bool     `json:"exists"`
	ExpectedBytes  int64    `json:"expected_bytes,omitempty"`
	ActualBytes    int64    `json:"actual_bytes,omitempty"`
	ExpectedSHA256 string   `json:"expected_sha256,omitempty"`
	ActualSHA256   string   `json:"actual_sha256,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

type snapshotLifecycleIndexDiffReport struct {
	OK               bool                               `json:"ok"`
	OldIndex         string                             `json:"old_index"`
	NewIndex         string                             `json:"new_index"`
	OldBundles       int                                `json:"old_bundles"`
	NewBundles       int                                `json:"new_bundles"`
	AddedBundles     []string                           `json:"added_bundles,omitempty"`
	RemovedBundles   []string                           `json:"removed_bundles,omitempty"`
	ChangedBundles   []snapshotLifecycleIndexBundleDiff `json:"changed_bundles,omitempty"`
	ValidationErrors []string                           `json:"validation_errors,omitempty"`
}

type snapshotLifecycleIndexBundleDiff struct {
	Bundle          string                               `json:"bundle"`
	ArtifactChanges []snapshotLifecycleIndexArtifactDiff `json:"artifact_changes,omitempty"`
	DecisionChanged bool                                 `json:"decision_changed,omitempty"`
	OldDecision     string                               `json:"old_decision,omitempty"`
	NewDecision     string                               `json:"new_decision,omitempty"`
}

type snapshotLifecycleIndexArtifactDiff struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Change    string `json:"change"`
	OldBytes  int64  `json:"old_bytes,omitempty"`
	NewBytes  int64  `json:"new_bytes,omitempty"`
	OldSHA256 string `json:"old_sha256,omitempty"`
	NewSHA256 string `json:"new_sha256,omitempty"`
}

type snapshotLifecycleIndexPublishReport struct {
	OK               bool                                 `json:"ok"`
	DryRun           bool                                 `json:"dry_run"`
	Published        bool                                 `json:"published"`
	IndexFile        string                               `json:"index_file"`
	PublishURL       string                               `json:"publish_url,omitempty"`
	ReceiptFile      string                               `json:"receipt_file,omitempty"`
	Method           string                               `json:"method,omitempty"`
	Status           string                               `json:"status,omitempty"`
	Response         string                               `json:"response,omitempty"`
	Payload          snapshotLifecycleIndexPublishPayload `json:"payload"`
	ValidationErrors []string                             `json:"validation_errors,omitempty"`
}

type snapshotLifecycleIndexPublishReceipt struct {
	Kind          string                               `json:"kind"`
	SchemaVersion int                                  `json:"schema_version"`
	GeneratedAt   string                               `json:"generated_at"`
	IndexFile     string                               `json:"index_file"`
	PublishURL    string                               `json:"publish_url"`
	Method        string                               `json:"method"`
	Status        string                               `json:"status"`
	Response      string                               `json:"response,omitempty"`
	PayloadSHA256 string                               `json:"payload_sha256"`
	Payload       snapshotLifecycleIndexPublishPayload `json:"payload"`
}

type snapshotLifecycleIndexPublishReceiptVerifyReport struct {
	OK                    bool                                 `json:"ok"`
	ReceiptFile           string                               `json:"receipt_file"`
	IndexFile             string                               `json:"index_file"`
	ReceiptKind           string                               `json:"receipt_kind"`
	ReceiptGeneratedAt    string                               `json:"receipt_generated_at,omitempty"`
	ReceiptIndexFile      string                               `json:"receipt_index_file,omitempty"`
	ReceiptStatus         string                               `json:"receipt_status,omitempty"`
	ReceiptPayloadSHA256  string                               `json:"receipt_payload_sha256,omitempty"`
	ExpectedPayloadSHA256 string                               `json:"expected_payload_sha256,omitempty"`
	ReceiptPayload        snapshotLifecycleIndexPublishPayload `json:"receipt_payload"`
	ExpectedPayload       snapshotLifecycleIndexPublishPayload `json:"expected_payload"`
	ValidationErrors      []string                             `json:"validation_errors,omitempty"`
}

type snapshotLifecycleIndexPublishDriftReport struct {
	OK                        bool                                 `json:"ok"`
	Drift                     bool                                 `json:"drift"`
	IndexFile                 string                               `json:"index_file"`
	PublishedURL              string                               `json:"published_url,omitempty"`
	Method                    string                               `json:"method,omitempty"`
	Status                    string                               `json:"status,omitempty"`
	RecommendedPublishCommand string                               `json:"recommended_publish_command,omitempty"`
	LocalPayload              snapshotLifecycleIndexPublishPayload `json:"local_payload"`
	PublishedPayload          snapshotLifecycleIndexPublishPayload `json:"published_payload"`
	Differences               []string                             `json:"differences,omitempty"`
	ValidationErrors          []string                             `json:"validation_errors,omitempty"`
}

type snapshotLifecycleIndexPublishFreshnessReport struct {
	OK               bool                                 `json:"ok"`
	Fresh            bool                                 `json:"fresh"`
	PublishedURL     string                               `json:"published_url,omitempty"`
	Method           string                               `json:"method,omitempty"`
	Status           string                               `json:"status,omitempty"`
	GeneratedAt      string                               `json:"generated_at,omitempty"`
	CheckedAt        string                               `json:"checked_at"`
	MaxAgeSeconds    int64                                `json:"max_age_seconds"`
	AgeSeconds       int64                                `json:"age_seconds,omitempty"`
	PublishedPayload snapshotLifecycleIndexPublishPayload `json:"published_payload"`
	ValidationErrors []string                             `json:"validation_errors,omitempty"`
}

type snapshotLifecycleIndexPublishPayload struct {
	Kind             string                                       `json:"kind"`
	SchemaVersion    int                                          `json:"schema_version"`
	IndexFile        string                                       `json:"index_file"`
	GeneratedAt      string                                       `json:"generated_at"`
	ContextDBVersion string                                       `json:"contextdb_version"`
	Dir              string                                       `json:"dir"`
	Namespace        string                                       `json:"namespace,omitempty"`
	Keep             int                                          `json:"keep"`
	TotalBundles     int                                          `json:"total_bundles"`
	KeepBundles      int                                          `json:"keep_bundles"`
	PruneableBundles int                                          `json:"pruneable_bundles"`
	Bundles          []snapshotLifecycleIndexPublishBundleSummary `json:"bundles"`
}

type snapshotLifecycleIndexPublishBundleSummary struct {
	Namespace      string `json:"namespace"`
	CreatedAt      string `json:"created_at"`
	Summary        string `json:"summary"`
	Promoted       bool   `json:"promoted"`
	Decision       string `json:"decision"`
	ArtifactCount  int    `json:"artifact_count"`
	ExistingBytes  int64  `json:"existing_bytes,omitempty"`
	IndexedSHA256s int    `json:"indexed_sha256s,omitempty"`
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

type rankingEvalBaselineVersion struct {
	Path  string
	Major int
	Minor int
	Patch int
}

type kvRefreshOptions struct {
	Keys        []string
	Value       []byte
	ValueSource string
	TTLSeconds  int
	Overwrite   bool
	Execute     bool
	GeneratedAt time.Time
}

type kvRefreshValueOptions struct {
	Value           string
	ValueFile       string
	Derive          string
	DeriveNamespace string
	DeriveLabels    []string
	DeriveLimit     int
}

type kvRefreshRecentNodesValue struct {
	Kind        string                     `json:"kind"`
	Namespace   string                     `json:"namespace"`
	GeneratedAt string                     `json:"generated_at"`
	Limit       int                        `json:"limit"`
	Labels      []string                   `json:"labels,omitempty"`
	Count       int                        `json:"count"`
	Nodes       []kvRefreshRecentNodeValue `json:"nodes"`
}

type kvRefreshRecentNodeValue struct {
	ID            string         `json:"id"`
	TxTime        string         `json:"tx_time,omitempty"`
	ValidFrom     string         `json:"valid_from,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	Text          string         `json:"text,omitempty"`
	Confidence    float64        `json:"confidence,omitempty"`
	EpistemicType string         `json:"epistemic_type,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
}

type kvRefreshReport struct {
	SchemaVersion     int                 `json:"schema_version"`
	ContextDBVersion  string              `json:"contextdb_version"`
	GeneratedAt       string              `json:"generated_at"`
	DryRun            bool                `json:"dry_run"`
	Execute           bool                `json:"execute"`
	Overwrite         bool                `json:"overwrite"`
	TTLSeconds        int                 `json:"ttl_seconds"`
	ValueSource       string              `json:"value_source"`
	Keys              int                 `json:"keys"`
	Present           int                 `json:"present"`
	Missing           int                 `json:"missing"`
	RefreshCandidates int                 `json:"refresh_candidates"`
	Written           int                 `json:"written"`
	Skipped           int                 `json:"skipped"`
	OK                bool                `json:"ok"`
	ValidationErrors  []string            `json:"validation_errors,omitempty"`
	Items             []kvRefreshPlanItem `json:"items"`
}

type kvRefreshPlanItem struct {
	Key        string `json:"key"`
	Present    bool   `json:"present"`
	Action     string `json:"action"`
	ValueBytes int    `json:"value_bytes,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
	Error      string `json:"error,omitempty"`
}

type kvDerivedFreshnessValue struct {
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace,omitempty"`
	GeneratedAt string `json:"generated_at"`
}

type vectorIndexRepairReport struct {
	SchemaVersion    int      `json:"schema_version"`
	ContextDBVersion string   `json:"contextdb_version"`
	GeneratedAt      string   `json:"generated_at"`
	Namespace        string   `json:"namespace"`
	SampleLimit      int      `json:"sample_limit"`
	SampledNodes     int      `json:"sampled_nodes"`
	VectorNodes      int      `json:"vector_nodes"`
	CandidateIDs     []string `json:"candidate_ids"`
	ReindexedIDs     []string `json:"reindexed_ids,omitempty"`
	DryRun           bool     `json:"dry_run"`
	OK               bool     `json:"ok"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
}

func writeSnapshotArtifactManifest(path string, opts snapshotArtifactManifestOptions) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	manifest, err := buildSnapshotArtifactManifest(opts)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode artifact manifest: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeSnapshotPromotionReceipt(path string, opts snapshotPromotionReceiptOptions) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	receipt := buildSnapshotPromotionReceipt(opts)
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode promotion receipt: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func buildSnapshotPromotionReceipt(opts snapshotPromotionReceiptOptions) snapshotPromotionReceipt {
	return snapshotPromotionReceipt{
		SchemaVersion:    1,
		Namespace:        opts.Namespace,
		BackupFile:       strings.TrimSpace(opts.BackupPath),
		PromotedAt:       opts.ImportedAt.UTC().Format(time.RFC3339),
		ContextDBVersion: buildinfo.Version,
		PromotionNote:    strings.TrimSpace(opts.Note),
		ImportReport:     opts.Report,
	}
}

func verifySnapshotPromotionReceipt(promotionReportPath, manifestPath string) (snapshotPromotionReceiptVerifyReport, error) {
	promotionReportPath = strings.TrimSpace(promotionReportPath)
	manifestPath = strings.TrimSpace(manifestPath)
	if promotionReportPath == "" {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("--promotion-report is required")
	}
	if manifestPath == "" {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("--manifest is required")
	}
	receiptData, err := os.ReadFile(promotionReportPath)
	if err != nil {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("read promotion receipt: %w", err)
	}
	var receipt snapshotPromotionReceipt
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("decode promotion receipt: %w", err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("read artifact manifest: %w", err)
	}
	var manifest snapshotArtifactManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return snapshotPromotionReceiptVerifyReport{}, fmt.Errorf("decode artifact manifest: %w", err)
	}
	importedRecords := snapshotArtifactCounts{
		Lines:   receipt.ImportReport.Lines,
		Nodes:   receipt.ImportReport.Nodes,
		Edges:   receipt.ImportReport.Edges,
		Sources: receipt.ImportReport.Sources,
	}
	report := snapshotPromotionReceiptVerifyReport{
		PromotionReport:    promotionReportPath,
		Manifest:           manifestPath,
		ReceiptNamespace:   receipt.Namespace,
		ImportNamespace:    receipt.ImportReport.Namespace,
		ReceiptBackupFile:  receipt.BackupFile,
		ManifestBackupFile: manifest.BackupFile,
		ReceiptVersion:     receipt.ContextDBVersion,
		ManifestVersion:    manifest.ContextDBVersion,
		ImportedRecords:    importedRecords,
		ManifestRecords:    manifest.Records,
		PromotedAt:         receipt.PromotedAt,
	}
	if receipt.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported receipt schema_version %d", receipt.SchemaVersion))
	}
	if manifest.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported manifest schema_version %d", manifest.SchemaVersion))
	}
	if strings.TrimSpace(receipt.Namespace) == "" {
		report.ValidationErrors = append(report.ValidationErrors, "receipt namespace is empty")
	}
	if receipt.Namespace != receipt.ImportReport.Namespace {
		report.ValidationErrors = append(report.ValidationErrors, "receipt namespace does not match import report namespace")
	}
	if filepath.Base(strings.TrimSpace(receipt.BackupFile)) != strings.TrimSpace(manifest.BackupFile) {
		report.ValidationErrors = append(report.ValidationErrors, "receipt backup_file does not match manifest backup_file")
	}
	if importedRecords != manifest.Records {
		report.ValidationErrors = append(report.ValidationErrors, "import report record counts do not match manifest record counts")
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("promotion receipt verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func verifySnapshotLifecycleSummary(summaryPath string) (snapshotLifecycleVerifyReport, error) {
	summaryPath = strings.TrimSpace(summaryPath)
	if summaryPath == "" {
		return snapshotLifecycleVerifyReport{}, fmt.Errorf("--summary is required")
	}
	summaryData, err := os.ReadFile(summaryPath)
	if err != nil {
		return snapshotLifecycleVerifyReport{}, fmt.Errorf("read lifecycle summary: %w", err)
	}
	var summary snapshotLifecycleSummary
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		return snapshotLifecycleVerifyReport{}, fmt.Errorf("decode lifecycle summary: %w", err)
	}
	baseDir := filepath.Dir(summaryPath)
	backupPath := resolveLifecycleSummaryPath(baseDir, summary.Backup)
	manifestPath := resolveLifecycleSummaryPath(baseDir, summary.Manifest)
	rehearsalPath := resolveLifecycleSummaryPath(baseDir, summary.Rehearsal)
	promotionPath := resolveLifecycleSummaryPath(baseDir, summary.Promotion)
	receiptCheckPath := resolveLifecycleSummaryPath(baseDir, summary.ReceiptCheck)
	report := snapshotLifecycleVerifyReport{
		Summary:      summaryPath,
		Namespace:    summary.Namespace,
		CreatedAt:    summary.CreatedAt,
		Promoted:     summary.Promoted,
		Backup:       backupPath,
		Manifest:     manifestPath,
		Rehearsal:    rehearsalPath,
		Promotion:    promotionPath,
		ReceiptCheck: receiptCheckPath,
	}
	if strings.TrimSpace(summary.Namespace) == "" {
		report.ValidationErrors = append(report.ValidationErrors, "namespace is empty")
	}
	if strings.TrimSpace(summary.CreatedAt) == "" {
		report.ValidationErrors = append(report.ValidationErrors, "created_at is empty")
	}
	report.BackupExists = lifecycleFileExists(backupPath)
	if !report.BackupExists {
		report.ValidationErrors = append(report.ValidationErrors, "backup file is missing")
	}
	report.ManifestExists = lifecycleFileExists(manifestPath)
	if !report.ManifestExists {
		report.ValidationErrors = append(report.ValidationErrors, "manifest file is missing")
	}
	if report.BackupExists && report.ManifestExists {
		manifestReport, err := verifySnapshotArtifactManifest(manifestPath, backupPath)
		report.ManifestOK = manifestReport.OK
		if err != nil {
			report.ValidationErrors = append(report.ValidationErrors, err.Error())
		}
		if manifestReport.OK && strings.TrimSpace(manifestReport.Manifest) != "" {
			var manifest snapshotArtifactManifest
			if err := readJSONFile(manifestPath, &manifest); err != nil {
				report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode artifact manifest: %v", err))
			} else if strings.TrimSpace(manifest.Namespace) != "" && manifest.Namespace != summary.Namespace {
				report.ValidationErrors = append(report.ValidationErrors, "manifest namespace does not match lifecycle namespace")
			}
		}
	}
	report.RehearsalExists = lifecycleFileExists(rehearsalPath)
	if !report.RehearsalExists {
		report.ValidationErrors = append(report.ValidationErrors, "rehearsal report is missing")
	} else {
		var rehearsal snapshotRehearsalReport
		if err := readJSONFile(rehearsalPath, &rehearsal); err != nil {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode rehearsal report: %v", err))
		} else {
			report.RehearsalOK = rehearsal.OK && rehearsal.Verification.OK
			if !report.RehearsalOK {
				report.ValidationErrors = append(report.ValidationErrors, "rehearsal report is not ok")
			}
		}
	}
	if summary.Promoted {
		report.PromotionExists = lifecycleFileExists(promotionPath)
		if !report.PromotionExists {
			report.ValidationErrors = append(report.ValidationErrors, "promotion receipt is missing")
		}
		report.ReceiptCheckOK = false
		if !lifecycleFileExists(receiptCheckPath) {
			report.ValidationErrors = append(report.ValidationErrors, "receipt verification report is missing")
		} else {
			var receiptCheck snapshotPromotionReceiptVerifyReport
			if err := readJSONFile(receiptCheckPath, &receiptCheck); err != nil {
				report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode receipt verification report: %v", err))
			} else {
				report.ReceiptCheckOK = receiptCheck.OK
				if !receiptCheck.OK {
					report.ValidationErrors = append(report.ValidationErrors, "receipt verification report is not ok")
				}
			}
		}
		if report.PromotionExists && report.ManifestExists {
			promotionReport, err := verifySnapshotPromotionReceipt(promotionPath, manifestPath)
			report.PromotionOK = promotionReport.OK
			if err != nil {
				report.ValidationErrors = append(report.ValidationErrors, err.Error())
			}
			if promotionReport.OK && promotionReport.ImportNamespace != summary.Namespace {
				report.ValidationErrors = append(report.ValidationErrors, "promotion namespace does not match lifecycle namespace")
			}
		}
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("lifecycle summary verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func buildSnapshotLifecycleRetentionReport(dir, namespace string, keep int) (snapshotLifecycleRetentionReport, error) {
	dir = strings.TrimSpace(dir)
	namespace = strings.TrimSpace(namespace)
	report := snapshotLifecycleRetentionReport{
		Dir:       dir,
		Namespace: namespace,
		Keep:      keep,
	}
	if dir == "" {
		return report, fmt.Errorf("--dir is required")
	}
	if keep < 1 {
		report.ValidationErrors = append(report.ValidationErrors, "--keep must be at least 1")
		report.OK = false
		return report, fmt.Errorf("lifecycle retention report failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return report, fmt.Errorf("read lifecycle directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lifecycle.json") {
			continue
		}
		summaryPath := filepath.Join(dir, entry.Name())
		var summary snapshotLifecycleSummary
		if err := readJSONFile(summaryPath, &summary); err != nil {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode lifecycle summary %s: %v", summaryPath, err))
			continue
		}
		if namespace != "" && summary.Namespace != namespace {
			continue
		}
		info, _ := entry.Info()
		report.Bundles = append(report.Bundles, buildSnapshotLifecycleRetentionBundle(dir, summaryPath, summary, info))
	}
	sort.SliceStable(report.Bundles, func(i, j int) bool {
		if report.Bundles[i].sortTime.Equal(report.Bundles[j].sortTime) {
			return report.Bundles[i].Summary > report.Bundles[j].Summary
		}
		return report.Bundles[i].sortTime.After(report.Bundles[j].sortTime)
	})
	for i := range report.Bundles {
		if i < keep {
			report.Bundles[i].Decision = "keep"
			report.Bundles[i].Reason = "within newest lifecycle bundles to keep"
			report.KeepBundles++
		} else {
			report.Bundles[i].Decision = "pruneable"
			report.Bundles[i].Reason = "older than newest lifecycle bundles to keep"
			report.PruneableBundles++
		}
	}
	report.TotalBundles = len(report.Bundles)
	report.DeleteCommands = snapshotLifecycleDeleteCommands(report.Bundles)
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("lifecycle retention report failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func snapshotLifecycleDeleteCommands(bundles []snapshotLifecycleRetentionBundle) []string {
	seen := map[string]bool{}
	var paths []string
	for _, bundle := range bundles {
		if bundle.Decision != "pruneable" {
			continue
		}
		for _, artifact := range bundle.Artifacts {
			path := strings.TrimSpace(artifact.Path)
			if path == "" || !artifact.Exists || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	commands := make([]string, 0, len(paths))
	for _, path := range paths {
		commands = append(commands, "rm -- "+shellQuote(path))
	}
	return commands
}

func buildSnapshotLifecycleDeleteScript(report snapshotLifecycleRetentionReport) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n")
	b.WriteString("# Dry-run deletion plan generated by contextdb snapshot lifecycle retention.\n")
	b.WriteString("# Review every path before running these commands.\n")
	if len(report.DeleteCommands) == 0 {
		b.WriteString("# No pruneable artifacts were found.\n")
		return b.String()
	}
	for _, command := range report.DeleteCommands {
		b.WriteString(command)
		b.WriteByte('\n')
	}
	return b.String()
}

func buildSnapshotLifecycleRetentionBundle(baseDir, summaryPath string, summary snapshotLifecycleSummary, info os.FileInfo) snapshotLifecycleRetentionBundle {
	sortTime := time.Time{}
	if createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(summary.CreatedAt)); err == nil {
		sortTime = createdAt
	} else if info != nil {
		sortTime = info.ModTime()
	}
	return snapshotLifecycleRetentionBundle{
		Namespace: summary.Namespace,
		CreatedAt: summary.CreatedAt,
		Summary:   summaryPath,
		Promoted:  summary.Promoted,
		Artifacts: []snapshotLifecycleRetentionArtifact{
			snapshotLifecycleRetentionArtifactFor("summary", summaryPath),
			snapshotLifecycleRetentionArtifactFor("backup", resolveLifecycleSummaryPath(baseDir, summary.Backup)),
			snapshotLifecycleRetentionArtifactFor("manifest", resolveLifecycleSummaryPath(baseDir, summary.Manifest)),
			snapshotLifecycleRetentionArtifactFor("rehearsal", resolveLifecycleSummaryPath(baseDir, summary.Rehearsal)),
			snapshotLifecycleRetentionArtifactFor("promotion", resolveLifecycleSummaryPath(baseDir, summary.Promotion)),
			snapshotLifecycleRetentionArtifactFor("receipt_check", resolveLifecycleSummaryPath(baseDir, summary.ReceiptCheck)),
		},
		sortTime: sortTime,
	}
}

func snapshotLifecycleRetentionArtifactFor(kind, path string) snapshotLifecycleRetentionArtifact {
	artifact := snapshotLifecycleRetentionArtifact{
		Kind: kind,
		Path: strings.TrimSpace(path),
	}
	if artifact.Path == "" {
		return artifact
	}
	info, err := os.Stat(artifact.Path)
	if err == nil && !info.IsDir() {
		artifact.Exists = true
		artifact.Bytes = info.Size()
	}
	return artifact
}

func writeSnapshotLifecycleIndex(path string, opts snapshotLifecycleIndexOptions) (snapshotLifecycleIndex, error) {
	index, err := buildSnapshotLifecycleIndex(path, opts)
	if err != nil {
		return index, err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return index, fmt.Errorf("encode lifecycle index: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(index.IndexFile, data, 0o644); err != nil {
		return index, fmt.Errorf("write lifecycle index: %w", err)
	}
	return index, nil
}

func buildSnapshotLifecycleIndex(path string, opts snapshotLifecycleIndexOptions) (snapshotLifecycleIndex, error) {
	dir := strings.TrimSpace(opts.Dir)
	index := snapshotLifecycleIndex{
		SchemaVersion:    1,
		Dir:              dir,
		Namespace:        strings.TrimSpace(opts.Namespace),
		Keep:             opts.Keep,
		ContextDBVersion: buildinfo.Version,
	}
	if opts.CreatedAt.IsZero() {
		opts.CreatedAt = time.Now()
	}
	index.GeneratedAt = opts.CreatedAt.UTC().Format(time.RFC3339)
	path = strings.TrimSpace(path)
	if path == "" && dir != "" {
		path = filepath.Join(dir, "contextdb-backups.index.json")
	}
	index.IndexFile = path
	if path == "" {
		return index, fmt.Errorf("--out requires --dir or an explicit path")
	}
	report, err := buildSnapshotLifecycleRetentionReport(dir, opts.Namespace, opts.Keep)
	if err != nil {
		return index, err
	}
	index.Dir = report.Dir
	index.Namespace = report.Namespace
	index.Keep = report.Keep
	index.TotalBundles = report.TotalBundles
	index.KeepBundles = report.KeepBundles
	index.PruneableBundles = report.PruneableBundles
	index.DeleteCommands = report.DeleteCommands
	index.Bundles = report.Bundles
	addSnapshotLifecycleIndexHashes(index.Bundles)
	return index, nil
}

func diffSnapshotLifecycleIndexes(oldPath, newPath string) (snapshotLifecycleIndexDiffReport, error) {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	report := snapshotLifecycleIndexDiffReport{
		OldIndex: oldPath,
		NewIndex: newPath,
	}
	if oldPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--old is required")
	}
	if newPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--new is required")
	}
	if len(report.ValidationErrors) > 0 {
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	oldIndex, err := readSnapshotLifecycleIndex(oldPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read old lifecycle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	newIndex, err := readSnapshotLifecycleIndex(newPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read new lifecycle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.OldBundles = len(oldIndex.Bundles)
	report.NewBundles = len(newIndex.Bundles)
	if oldIndex.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported old index schema_version %d", oldIndex.SchemaVersion))
	}
	if newIndex.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported new index schema_version %d", newIndex.SchemaVersion))
	}

	oldBundles := snapshotLifecycleBundleMap(oldIndex.Bundles)
	newBundles := snapshotLifecycleBundleMap(newIndex.Bundles)
	for _, key := range sortedStringKeys(newBundles) {
		if _, ok := oldBundles[key]; !ok {
			report.AddedBundles = append(report.AddedBundles, key)
		}
	}
	for _, key := range sortedStringKeys(oldBundles) {
		oldBundle := oldBundles[key]
		newBundle, ok := newBundles[key]
		if !ok {
			report.RemovedBundles = append(report.RemovedBundles, key)
			continue
		}
		if diff, changed := diffSnapshotLifecycleIndexBundle(key, oldBundle, newBundle); changed {
			report.ChangedBundles = append(report.ChangedBundles, diff)
		}
	}
	report.OK = len(report.ValidationErrors) == 0 &&
		len(report.AddedBundles) == 0 &&
		len(report.RemovedBundles) == 0 &&
		len(report.ChangedBundles) == 0
	if !report.OK {
		parts := append([]string{}, report.ValidationErrors...)
		if len(report.AddedBundles) > 0 {
			parts = append(parts, fmt.Sprintf("%d added bundle(s)", len(report.AddedBundles)))
		}
		if len(report.RemovedBundles) > 0 {
			parts = append(parts, fmt.Sprintf("%d removed bundle(s)", len(report.RemovedBundles)))
		}
		if len(report.ChangedBundles) > 0 {
			parts = append(parts, fmt.Sprintf("%d changed bundle(s)", len(report.ChangedBundles)))
		}
		return report, fmt.Errorf("lifecycle index diff found changes: %s", strings.Join(parts, "; "))
	}
	return report, nil
}

func buildSnapshotLifecycleIndexPublishReport(ctx context.Context, client *http.Client, path string, opts snapshotLifecycleIndexPublishOptions) (snapshotLifecycleIndexPublishReport, error) {
	path = strings.TrimSpace(path)
	report := snapshotLifecycleIndexPublishReport{
		DryRun:      opts.DryRun,
		IndexFile:   path,
		PublishURL:  strings.TrimSpace(opts.PublishURL),
		ReceiptFile: strings.TrimSpace(opts.ReceiptOut),
		Method:      strings.ToUpper(strings.TrimSpace(opts.Method)),
	}
	if report.Method == "" {
		report.Method = http.MethodPost
	}
	if path == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--in is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	index, err := readSnapshotLifecycleIndex(path)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read lifecycle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if index.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported index schema_version %d", index.SchemaVersion))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.Payload = buildSnapshotLifecycleIndexPublishPayload(index)
	if opts.DryRun && report.ReceiptFile != "" {
		report.ValidationErrors = append(report.ValidationErrors, "--receipt-out requires --execute")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if opts.DryRun {
		report.OK = true
		return report, nil
	}
	if report.PublishURL == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--publish-url or CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL is required when --execute is set")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	status, response, err := publishJSON(ctx, client, report.PublishURL, report.Method, strings.TrimSpace(opts.Token), report.Payload)
	report.Status = status
	report.Response = response
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, err
	}
	report.OK = true
	report.Published = true
	if report.ReceiptFile != "" {
		receipt, err := buildSnapshotLifecycleIndexPublishReceipt(report)
		if err != nil {
			report.ValidationErrors = append(report.ValidationErrors, err.Error())
			report.OK = false
			return report, err
		}
		if err := writeJSONFile(report.ReceiptFile, receipt); err != nil {
			err = fmt.Errorf("write publish receipt: %w", err)
			report.ValidationErrors = append(report.ValidationErrors, err.Error())
			report.OK = false
			return report, err
		}
	}
	return report, nil
}

func buildSnapshotLifecycleIndexPublishReceipt(report snapshotLifecycleIndexPublishReport) (snapshotLifecycleIndexPublishReceipt, error) {
	payloadSHA, err := snapshotLifecycleIndexPublishPayloadSHA256(report.Payload)
	if err != nil {
		return snapshotLifecycleIndexPublishReceipt{}, fmt.Errorf("encode publish receipt payload hash: %w", err)
	}
	return snapshotLifecycleIndexPublishReceipt{
		Kind:          "contextdb.lifecycle.index.publish.receipt",
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		IndexFile:     report.IndexFile,
		PublishURL:    report.PublishURL,
		Method:        report.Method,
		Status:        report.Status,
		Response:      report.Response,
		PayloadSHA256: payloadSHA,
		Payload:       report.Payload,
	}, nil
}

func verifySnapshotLifecycleIndexPublishReceipt(receiptPath, indexPath string) (snapshotLifecycleIndexPublishReceiptVerifyReport, error) {
	receiptPath = strings.TrimSpace(receiptPath)
	indexPath = strings.TrimSpace(indexPath)
	report := snapshotLifecycleIndexPublishReceiptVerifyReport{
		ReceiptFile: receiptPath,
		IndexFile:   indexPath,
	}
	if receiptPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--receipt is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if indexPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--in is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	receiptData, err := os.ReadFile(receiptPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read publish receipt: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	var receipt snapshotLifecycleIndexPublishReceipt
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode publish receipt: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	index, err := readSnapshotLifecycleIndex(indexPath)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read lifecycle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	expectedPayload := buildSnapshotLifecycleIndexPublishPayload(index)
	expectedSHA, err := snapshotLifecycleIndexPublishPayloadSHA256(expectedPayload)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	receiptSHA, err := snapshotLifecycleIndexPublishPayloadSHA256(receipt.Payload)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.ReceiptKind = receipt.Kind
	report.ReceiptGeneratedAt = receipt.GeneratedAt
	report.ReceiptIndexFile = receipt.IndexFile
	report.ReceiptStatus = receipt.Status
	report.ReceiptPayloadSHA256 = strings.TrimSpace(receipt.PayloadSHA256)
	report.ExpectedPayloadSHA256 = expectedSHA
	report.ReceiptPayload = receipt.Payload
	report.ExpectedPayload = expectedPayload
	if receipt.Kind != "contextdb.lifecycle.index.publish.receipt" {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported receipt kind %q", receipt.Kind))
	}
	if receipt.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported receipt schema_version %d", receipt.SchemaVersion))
	}
	if strings.TrimSpace(receipt.PayloadSHA256) == "" {
		report.ValidationErrors = append(report.ValidationErrors, "receipt payload_sha256 is empty")
	} else if !strings.EqualFold(strings.TrimSpace(receipt.PayloadSHA256), receiptSHA) {
		report.ValidationErrors = append(report.ValidationErrors, "receipt payload_sha256 does not match receipt payload")
	}
	if !strings.EqualFold(receiptSHA, expectedSHA) || !reflect.DeepEqual(receipt.Payload, expectedPayload) {
		report.ValidationErrors = append(report.ValidationErrors, "receipt payload does not match lifecycle index publish payload")
	}
	if filepath.Base(strings.TrimSpace(receipt.IndexFile)) != filepath.Base(indexPath) {
		report.ValidationErrors = append(report.ValidationErrors, "receipt index_file does not match lifecycle index")
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("publish receipt verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func snapshotLifecycleIndexPublishPayloadSHA256(payload snapshotLifecycleIndexPublishPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode publish payload hash: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func buildSnapshotLifecycleIndexPublishPayload(index snapshotLifecycleIndex) snapshotLifecycleIndexPublishPayload {
	payload := snapshotLifecycleIndexPublishPayload{
		Kind:             "contextdb.lifecycle.index",
		SchemaVersion:    index.SchemaVersion,
		IndexFile:        filepath.Base(strings.TrimSpace(index.IndexFile)),
		GeneratedAt:      index.GeneratedAt,
		ContextDBVersion: index.ContextDBVersion,
		Dir:              filepath.Base(strings.TrimSpace(index.Dir)),
		Namespace:        index.Namespace,
		Keep:             index.Keep,
		TotalBundles:     index.TotalBundles,
		KeepBundles:      index.KeepBundles,
		PruneableBundles: index.PruneableBundles,
	}
	for _, bundle := range index.Bundles {
		summary := snapshotLifecycleIndexPublishBundleSummary{
			Namespace:     bundle.Namespace,
			CreatedAt:     bundle.CreatedAt,
			Summary:       filepath.Base(strings.TrimSpace(bundle.Summary)),
			Promoted:      bundle.Promoted,
			Decision:      bundle.Decision,
			ArtifactCount: len(bundle.Artifacts),
		}
		for _, artifact := range bundle.Artifacts {
			if artifact.Exists {
				summary.ExistingBytes += artifact.Bytes
			}
			if strings.TrimSpace(artifact.ChecksumSHA256) != "" {
				summary.IndexedSHA256s++
			}
		}
		payload.Bundles = append(payload.Bundles, summary)
	}
	return payload
}

func buildSnapshotLifecycleIndexPublishDriftReport(ctx context.Context, client *http.Client, path string, opts snapshotLifecycleIndexPublishDriftOptions) (snapshotLifecycleIndexPublishDriftReport, error) {
	path = strings.TrimSpace(path)
	report := snapshotLifecycleIndexPublishDriftReport{
		IndexFile:    path,
		PublishedURL: strings.TrimSpace(opts.PublishedURL),
		Method:       strings.ToUpper(strings.TrimSpace(opts.Method)),
	}
	if report.Method == "" {
		report.Method = http.MethodGet
	}
	if path == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--in is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if report.PublishedURL == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--published-url or CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	index, err := readSnapshotLifecycleIndex(path)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read lifecycle index: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if index.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported index schema_version %d", index.SchemaVersion))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.LocalPayload = buildSnapshotLifecycleIndexPublishPayload(index)
	published, status, err := fetchSnapshotLifecycleIndexPublishedPayload(ctx, client, report.PublishedURL, report.Method, opts.Token)
	report.Status = status
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, err
	}
	report.PublishedPayload = published
	report.Differences = diffSnapshotLifecycleIndexPublishPayloads(report.LocalPayload, report.PublishedPayload)
	report.Drift = len(report.Differences) > 0
	report.OK = !report.Drift
	if report.Drift {
		report.RecommendedPublishCommand = recommendedSnapshotLifecycleIndexPublishCommand(path, report.PublishedURL)
		return report, fmt.Errorf("published lifecycle index drift found: %s", strings.Join(report.Differences, "; "))
	}
	return report, nil
}

func recommendedSnapshotLifecycleIndexPublishCommand(indexPath, publishURL string) string {
	command := "contextdb snapshot lifecycle index publish --in " + shellQuote(indexPath) + " --report"
	if strings.TrimSpace(publishURL) != "" {
		command += " --publish-url " + shellQuote(publishURL)
	}
	return command
}

func buildSnapshotLifecycleIndexPublishFreshnessReport(ctx context.Context, client *http.Client, opts snapshotLifecycleIndexPublishFreshnessOptions) (snapshotLifecycleIndexPublishFreshnessReport, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	report := snapshotLifecycleIndexPublishFreshnessReport{
		PublishedURL:  strings.TrimSpace(opts.PublishedURL),
		Method:        strings.ToUpper(strings.TrimSpace(opts.Method)),
		CheckedAt:     now.UTC().Format(time.RFC3339),
		MaxAgeSeconds: int64(maxAge.Seconds()),
	}
	if report.Method == "" {
		report.Method = http.MethodGet
	}
	if report.PublishedURL == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--published-url or CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL is required")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	published, status, err := fetchSnapshotLifecycleIndexPublishedPayload(ctx, client, report.PublishedURL, report.Method, opts.Token)
	report.Status = status
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, err
	}
	report.PublishedPayload = published
	generatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(published.GeneratedAt))
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("published generated_at is invalid: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	report.GeneratedAt = generatedAt.UTC().Format(time.RFC3339)
	age := now.Sub(generatedAt)
	if age < 0 {
		age = 0
	}
	report.AgeSeconds = int64(age.Seconds())
	report.Fresh = age <= maxAge
	report.OK = report.Fresh
	if !report.Fresh {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("published generated_at age %s exceeds max age %s", age.Round(time.Second), maxAge.Round(time.Second)))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func fetchSnapshotLifecycleIndexPublishedPayload(ctx context.Context, client *http.Client, publishedURL, method, token string) (snapshotLifecycleIndexPublishPayload, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, method, publishedURL, nil)
	if err != nil {
		return snapshotLifecycleIndexPublishPayload{}, "", err
	}
	req.Header.Set("Accept", "application/json")
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return snapshotLifecycleIndexPublishPayload{}, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return snapshotLifecycleIndexPublishPayload{}, resp.Status, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return snapshotLifecycleIndexPublishPayload{}, resp.Status, fmt.Errorf("published metadata returned status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	payload, err := decodeSnapshotLifecycleIndexPublishedPayload(body)
	if err != nil {
		return snapshotLifecycleIndexPublishPayload{}, resp.Status, err
	}
	return payload, resp.Status, nil
}

func decodeSnapshotLifecycleIndexPublishedPayload(body []byte) (snapshotLifecycleIndexPublishPayload, error) {
	var payload snapshotLifecycleIndexPublishPayload
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Kind) != "" {
		return payload, nil
	}
	var wrapped struct {
		Payload snapshotLifecycleIndexPublishPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return snapshotLifecycleIndexPublishPayload{}, fmt.Errorf("decode published metadata: %w", err)
	}
	if strings.TrimSpace(wrapped.Payload.Kind) == "" {
		return snapshotLifecycleIndexPublishPayload{}, errors.New("decode published metadata: missing payload kind")
	}
	return wrapped.Payload, nil
}

func diffSnapshotLifecycleIndexPublishPayloads(local, published snapshotLifecycleIndexPublishPayload) []string {
	var diffs []string
	compareString := func(name, a, b string) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s differs: local=%q published=%q", name, a, b))
		}
	}
	compareInt := func(name string, a, b int) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s differs: local=%d published=%d", name, a, b))
		}
	}
	compareString("kind", local.Kind, published.Kind)
	compareInt("schema_version", local.SchemaVersion, published.SchemaVersion)
	compareString("contextdb_version", local.ContextDBVersion, published.ContextDBVersion)
	compareString("namespace", local.Namespace, published.Namespace)
	compareInt("keep", local.Keep, published.Keep)
	compareInt("total_bundles", local.TotalBundles, published.TotalBundles)
	compareInt("keep_bundles", local.KeepBundles, published.KeepBundles)
	compareInt("pruneable_bundles", local.PruneableBundles, published.PruneableBundles)

	localBundles := map[string]snapshotLifecycleIndexPublishBundleSummary{}
	for _, bundle := range local.Bundles {
		localBundles[publishBundleKey(bundle)] = bundle
	}
	publishedBundles := map[string]snapshotLifecycleIndexPublishBundleSummary{}
	for _, bundle := range published.Bundles {
		publishedBundles[publishBundleKey(bundle)] = bundle
	}
	for key, localBundle := range localBundles {
		publishedBundle, ok := publishedBundles[key]
		if !ok {
			diffs = append(diffs, "bundle missing from published payload: "+key)
			continue
		}
		if localBundle.Promoted != publishedBundle.Promoted {
			diffs = append(diffs, fmt.Sprintf("bundle %s promoted differs: local=%t published=%t", key, localBundle.Promoted, publishedBundle.Promoted))
		}
		if localBundle.Decision != publishedBundle.Decision {
			diffs = append(diffs, fmt.Sprintf("bundle %s decision differs: local=%q published=%q", key, localBundle.Decision, publishedBundle.Decision))
		}
		if localBundle.ArtifactCount != publishedBundle.ArtifactCount {
			diffs = append(diffs, fmt.Sprintf("bundle %s artifact_count differs: local=%d published=%d", key, localBundle.ArtifactCount, publishedBundle.ArtifactCount))
		}
		if localBundle.ExistingBytes != publishedBundle.ExistingBytes {
			diffs = append(diffs, fmt.Sprintf("bundle %s existing_bytes differs: local=%d published=%d", key, localBundle.ExistingBytes, publishedBundle.ExistingBytes))
		}
		if localBundle.IndexedSHA256s != publishedBundle.IndexedSHA256s {
			diffs = append(diffs, fmt.Sprintf("bundle %s indexed_sha256s differs: local=%d published=%d", key, localBundle.IndexedSHA256s, publishedBundle.IndexedSHA256s))
		}
	}
	for key := range publishedBundles {
		if _, ok := localBundles[key]; !ok {
			diffs = append(diffs, "bundle missing from local payload: "+key)
		}
	}
	sort.Strings(diffs)
	return diffs
}

func publishBundleKey(bundle snapshotLifecycleIndexPublishBundleSummary) string {
	return bundle.Namespace + "\x00" + bundle.CreatedAt + "\x00" + bundle.Summary
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

func artifactManifestValidationPrefix(item rankingEvalBaselineArtifactManifestVerifyReportItem, msg string) string {
	label := strings.TrimSpace(item.Kind)
	if label == "" {
		label = "artifact"
	}
	if strings.TrimSpace(item.Version) != "" {
		label = item.Version + " " + label
	}
	if strings.TrimSpace(item.Path) != "" {
		return fmt.Sprintf("%s %s: %s", label, item.Path, msg)
	}
	return fmt.Sprintf("%s: %s", label, msg)
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

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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

func markdownInline(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
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

func buildStoreConsistencyCheck(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, kv store.KVStore, namespace string, sampleLimit int) doctor.CheckResult {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	if sampleLimit <= 0 {
		sampleLimit = 100
	}
	nodes, err := graph.ValidAt(ctx, namespace, time.Now(), nil)
	if err != nil {
		return doctor.CheckResult{Name: "store_consistency", OK: false, Detail: "graph scan: " + err.Error()}
	}
	if len(nodes) > sampleLimit {
		nodes = nodes[:sampleLimit]
	}
	fingerprintChecked := 0
	vectorChecked := 0
	var issues []string
	for _, node := range nodes {
		if node.Fingerprint != "" {
			fingerprintChecked++
			found, err := graph.GetNodeByFingerprint(ctx, namespace, node.Fingerprint)
			if err != nil {
				issues = append(issues, fmt.Sprintf("fingerprint lookup failed for %s: %v", node.ID, err))
			} else if found == nil || found.ID != node.ID {
				issues = append(issues, fmt.Sprintf("fingerprint lookup mismatch for %s", node.ID))
			}
		}
		if len(node.Vector) > 0 {
			vectorChecked++
			results, err := vecs.Search(ctx, store.VectorQuery{
				Namespace: namespace,
				Vector:    node.Vector,
				TopK:      10,
				AsOf:      time.Now(),
			})
			if err != nil {
				issues = append(issues, fmt.Sprintf("vector search failed for %s: %v", node.ID, err))
			} else if !scoredResultsContainNode(results, node.ID) {
				issues = append(issues, fmt.Sprintf("vector rebuild candidate %s", node.ID))
			}
		}
	}
	if kv == nil {
		issues = append(issues, "kv store unavailable")
	}
	detail := fmt.Sprintf("namespace=%s sampled=%d fingerprints=%d vectors=%d rebuild_candidates=%d", namespace, len(nodes), fingerprintChecked, vectorChecked, len(issues))
	if len(issues) > 0 {
		return doctor.CheckResult{Name: "store_consistency", OK: false, Detail: detail + ": " + strings.Join(issues, "; ")}
	}
	return doctor.CheckResult{Name: "store_consistency", OK: true, Detail: detail}
}

func buildKVConsistencyCheck(ctx context.Context, kv store.KVStore, keys []string) doctor.CheckResult {
	var normalized []string
	seen := map[string]struct{}{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	if len(normalized) == 0 {
		return doctor.CheckResult{Name: "kv_consistency", OK: true, Detail: "keys=0 present=0 missing=0 refresh_candidates=0"}
	}
	if kv == nil {
		return doctor.CheckResult{Name: "kv_consistency", OK: false, Detail: fmt.Sprintf("keys=%d present=0 missing=%d refresh_candidates=%d: kv store unavailable", len(normalized), len(normalized), len(normalized))}
	}
	present := 0
	var issues []string
	for _, key := range normalized {
		value, err := kv.Get(ctx, key)
		if err != nil {
			issues = append(issues, fmt.Sprintf("kv lookup failed for %q: %v", key, err))
			continue
		}
		if len(value) == 0 {
			issues = append(issues, fmt.Sprintf("kv refresh candidate %q", key))
			continue
		}
		present++
	}
	missing := len(normalized) - present
	detail := fmt.Sprintf("keys=%d present=%d missing=%d refresh_candidates=%d", len(normalized), present, missing, len(issues))
	if len(issues) > 0 {
		return doctor.CheckResult{Name: "kv_consistency", OK: false, Detail: detail + ": " + strings.Join(issues, "; ")}
	}
	return doctor.CheckResult{Name: "kv_consistency", OK: true, Detail: detail}
}

func buildKVDerivedFreshnessCheck(ctx context.Context, kv store.KVStore, keys []string, maxAge time.Duration, now time.Time) doctor.CheckResult {
	normalized := normalizeKVRefreshKeys(keys)
	if len(normalized) == 0 {
		return doctor.CheckResult{Name: "kv_derived_freshness", OK: true, Detail: "keys=0 fresh=0 stale=0 missing=0 invalid=0"}
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	if now.IsZero() {
		now = time.Now()
	}
	if kv == nil {
		return doctor.CheckResult{Name: "kv_derived_freshness", OK: false, Detail: fmt.Sprintf("keys=%d fresh=0 stale=0 missing=%d invalid=0 max_age_seconds=%d: kv store unavailable", len(normalized), len(normalized), int(maxAge.Seconds()))}
	}
	fresh := 0
	stale := 0
	missing := 0
	invalid := 0
	var issues []string
	var derivedNamespaces []string
	for _, key := range normalized {
		data, err := kv.Get(ctx, key)
		if err != nil {
			invalid++
			issues = append(issues, fmt.Sprintf("kv lookup failed for %q: %v", key, err))
			continue
		}
		if len(data) == 0 {
			missing++
			issues = append(issues, fmt.Sprintf("derived kv value missing %q", key))
			continue
		}
		var value kvDerivedFreshnessValue
		if err := json.Unmarshal(data, &value); err != nil {
			invalid++
			issues = append(issues, fmt.Sprintf("derived kv value %q is invalid JSON: %v", key, err))
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(value.Kind), "contextdb.kv.derived.") {
			invalid++
			issues = append(issues, fmt.Sprintf("derived kv value %q has unsupported kind %q", key, value.Kind))
			continue
		}
		if strings.TrimSpace(value.Namespace) != "" {
			derivedNamespaces = append(derivedNamespaces, strings.TrimSpace(value.Namespace))
		}
		generatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value.GeneratedAt))
		if err != nil {
			invalid++
			issues = append(issues, fmt.Sprintf("derived kv value %q generated_at is invalid: %v", key, err))
			continue
		}
		age := now.Sub(generatedAt)
		if age < 0 {
			age = 0
		}
		if age > maxAge {
			stale++
			issues = append(issues, fmt.Sprintf("derived kv value %q age %s exceeds max age %s", key, age.Round(time.Second), maxAge.Round(time.Second)))
			continue
		}
		fresh++
	}
	detail := fmt.Sprintf("keys=%d fresh=%d stale=%d missing=%d invalid=%d max_age_seconds=%d", len(normalized), fresh, stale, missing, invalid, int(maxAge.Seconds()))
	if len(issues) > 0 {
		detail += "; recommended_repair_command=" + recommendedKVDerivedFreshnessRepairCommand(normalized, derivedNamespaces)
		return doctor.CheckResult{Name: "kv_derived_freshness", OK: false, Detail: detail + ": " + strings.Join(issues, "; ")}
	}
	return doctor.CheckResult{Name: "kv_derived_freshness", OK: true, Detail: detail}
}

func recommendedKVDerivedFreshnessRepairCommand(keys, namespaces []string) string {
	command := "contextdb repair kv-cache"
	for _, key := range keys {
		command += " --key " + shellQuote(key)
	}
	command += " --derive recent-nodes --derive-namespace " + shellQuote(preferredKVDerivedFreshnessNamespace(keys, namespaces)) + " --report"
	return command
}

func preferredKVDerivedFreshnessNamespace(keys, namespaces []string) string {
	for _, namespace := range namespaces {
		if strings.TrimSpace(namespace) != "" {
			return strings.TrimSpace(namespace)
		}
	}
	for _, key := range keys {
		parts := strings.Split(strings.TrimSpace(key), ":")
		if len(parts) >= 4 && strings.TrimSpace(parts[2]) != "" {
			return strings.TrimSpace(parts[2])
		}
		if len(parts) >= 3 && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1])
		}
	}
	return "default"
}

func buildKVRefreshReport(ctx context.Context, kv store.KVStore, opts kvRefreshOptions) (kvRefreshReport, error) {
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	report := kvRefreshReport{
		SchemaVersion:    1,
		ContextDBVersion: buildinfo.Version,
		GeneratedAt:      generatedAt.UTC().Format(time.RFC3339),
		DryRun:           !opts.Execute,
		Execute:          opts.Execute,
		Overwrite:        opts.Overwrite,
		TTLSeconds:       opts.TTLSeconds,
		ValueSource:      strings.TrimSpace(opts.ValueSource),
	}
	if report.ValueSource == "" {
		report.ValueSource = "literal"
	}
	keys := normalizeKVRefreshKeys(opts.Keys)
	report.Keys = len(keys)
	if len(keys) == 0 {
		report.ValidationErrors = append(report.ValidationErrors, "at least one --key is required")
	}
	if len(opts.Value) == 0 {
		report.ValidationErrors = append(report.ValidationErrors, "refresh value must not be empty")
	}
	if opts.TTLSeconds < 0 {
		report.ValidationErrors = append(report.ValidationErrors, "--ttl must be zero or positive")
	}
	if kv == nil {
		report.ValidationErrors = append(report.ValidationErrors, "kv store unavailable")
	}
	if len(report.ValidationErrors) > 0 {
		report.OK = false
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	for _, key := range keys {
		item := kvRefreshPlanItem{Key: key, ValueBytes: len(opts.Value), TTLSeconds: opts.TTLSeconds}
		value, err := kv.Get(ctx, key)
		if err != nil {
			item.Action = "error"
			item.Error = err.Error()
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("kv lookup failed for %q: %v", key, err))
			report.Items = append(report.Items, item)
			continue
		}
		item.Present = len(value) > 0
		if item.Present {
			report.Present++
		} else {
			report.Missing++
		}
		if item.Present && !opts.Overwrite {
			item.Action = "skip_present"
			report.Skipped++
			report.Items = append(report.Items, item)
			continue
		}
		report.RefreshCandidates++
		if !opts.Execute {
			item.Action = "plan_write"
			report.Items = append(report.Items, item)
			continue
		}
		if err := kv.Set(ctx, key, opts.Value, opts.TTLSeconds); err != nil {
			item.Action = "error"
			item.Error = err.Error()
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("kv write failed for %q: %v", key, err))
			report.Items = append(report.Items, item)
			continue
		}
		item.Action = "written"
		report.Written++
		report.Items = append(report.Items, item)
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func kvRefreshValue(ctx context.Context, graph store.GraphStore, opts kvRefreshValueOptions) ([]byte, string, error) {
	value := strings.TrimSpace(opts.Value)
	valueFile := strings.TrimSpace(opts.ValueFile)
	derive := strings.TrimSpace(opts.Derive)
	explicitSources := 0
	for _, source := range []string{value, valueFile, derive} {
		if source != "" {
			explicitSources++
		}
	}
	if explicitSources > 1 {
		return nil, "", errors.New("--value, --value-file, and --derive are mutually exclusive")
	}
	if valueFile != "" {
		data, err := os.ReadFile(valueFile)
		if err != nil {
			return nil, "", fmt.Errorf("read --value-file: %w", err)
		}
		return data, "file:" + valueFile, nil
	}
	if derive != "" {
		switch derive {
		case "recent-nodes":
			value, err := deriveKVRefreshRecentNodesValue(ctx, graph, opts)
			if err != nil {
				return nil, "", err
			}
			data, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return nil, "", fmt.Errorf("marshal derived recent-nodes value: %w", err)
			}
			data = append(data, '\n')
			return data, "derived:recent-nodes", nil
		default:
			return nil, "", fmt.Errorf("unsupported --derive %q", derive)
		}
	}
	return []byte(value), "literal", nil
}

func deriveKVRefreshRecentNodesValue(ctx context.Context, graph store.GraphStore, opts kvRefreshValueOptions) (kvRefreshRecentNodesValue, error) {
	if graph == nil {
		return kvRefreshRecentNodesValue{}, errors.New("graph store unavailable")
	}
	namespace := strings.TrimSpace(opts.DeriveNamespace)
	if namespace == "" {
		namespace = "default"
	}
	limit := opts.DeriveLimit
	if limit <= 0 {
		limit = 5
	}
	labels := normalizeKVRefreshKeys(opts.DeriveLabels)
	nodes, err := graph.ValidAt(ctx, namespace, time.Now(), labels)
	if err != nil {
		return kvRefreshRecentNodesValue{}, fmt.Errorf("derive recent-nodes: %w", err)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		left := nodes[i].TxTime
		if left.IsZero() {
			left = nodes[i].ValidFrom
		}
		right := nodes[j].TxTime
		if right.IsZero() {
			right = nodes[j].ValidFrom
		}
		return left.After(right)
	})
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	value := kvRefreshRecentNodesValue{
		Kind:        "contextdb.kv.derived.recent_nodes.v1",
		Namespace:   namespace,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Limit:       limit,
		Labels:      labels,
		Count:       len(nodes),
		Nodes:       make([]kvRefreshRecentNodeValue, 0, len(nodes)),
	}
	for _, node := range nodes {
		item := kvRefreshRecentNodeValue{
			ID:            node.ID.String(),
			Labels:        append([]string(nil), node.Labels...),
			Text:          core.NodeText(node),
			Confidence:    node.Confidence,
			EpistemicType: node.EpistemicType,
			Properties:    compactKVRefreshNodeProperties(node.Properties),
		}
		if !node.TxTime.IsZero() {
			item.TxTime = node.TxTime.UTC().Format(time.RFC3339)
		}
		if !node.ValidFrom.IsZero() {
			item.ValidFrom = node.ValidFrom.UTC().Format(time.RFC3339)
		}
		value.Nodes = append(value.Nodes, item)
	}
	return value, nil
}

func compactKVRefreshNodeProperties(properties map[string]any) map[string]any {
	if len(properties) == 0 {
		return nil
	}
	compact := make(map[string]any, len(properties))
	for key, value := range properties {
		switch key {
		case "text", "content":
			continue
		default:
			compact[key] = value
		}
	}
	if len(compact) == 0 {
		return nil
	}
	return compact
}

func normalizeKVRefreshKeys(keys []string) []string {
	var normalized []string
	seen := map[string]struct{}{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized
}

func buildPublishedBackupFreshnessCheck(ctx context.Context, client *http.Client, opts snapshotLifecycleIndexPublishFreshnessOptions) doctor.CheckResult {
	report, err := buildSnapshotLifecycleIndexPublishFreshnessReport(ctx, client, opts)
	detail := fmt.Sprintf("url=%s status=%s generated_at=%s age_seconds=%d max_age_seconds=%d",
		report.PublishedURL,
		report.Status,
		report.GeneratedAt,
		report.AgeSeconds,
		report.MaxAgeSeconds)
	if err != nil {
		if len(report.ValidationErrors) > 0 {
			detail += ": " + strings.Join(report.ValidationErrors, "; ")
		} else {
			detail += ": " + err.Error()
		}
		return doctor.CheckResult{Name: "published_backup_freshness", OK: false, Detail: strings.TrimSpace(detail)}
	}
	return doctor.CheckResult{Name: "published_backup_freshness", OK: true, Detail: strings.TrimSpace(detail)}
}

func buildPublishedBackupDriftCheck(ctx context.Context, client *http.Client, path string, opts snapshotLifecycleIndexPublishDriftOptions) doctor.CheckResult {
	report, err := buildSnapshotLifecycleIndexPublishDriftReport(ctx, client, path, opts)
	detail := fmt.Sprintf("index=%s url=%s status=%s drift=%t differences=%d",
		report.IndexFile,
		report.PublishedURL,
		report.Status,
		report.Drift,
		len(report.Differences))
	if err != nil {
		if len(report.Differences) > 0 {
			detail += ": " + strings.Join(report.Differences, "; ")
		} else if len(report.ValidationErrors) > 0 {
			detail += ": " + strings.Join(report.ValidationErrors, "; ")
		} else {
			detail += ": " + err.Error()
		}
		if strings.TrimSpace(report.RecommendedPublishCommand) != "" {
			detail += "; recommended_publish_command=" + report.RecommendedPublishCommand
		}
		return doctor.CheckResult{Name: "published_backup_drift", OK: false, Detail: strings.TrimSpace(detail)}
	}
	return doctor.CheckResult{Name: "published_backup_drift", OK: true, Detail: strings.TrimSpace(detail)}
}

func buildVectorIndexRepairReport(ctx context.Context, graph store.GraphStore, vecs store.VectorIndex, namespace string, sampleLimit int, execute bool) (vectorIndexRepairReport, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	if sampleLimit <= 0 {
		sampleLimit = 100
	}
	report := vectorIndexRepairReport{
		SchemaVersion:    1,
		ContextDBVersion: buildinfo.Version,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		Namespace:        namespace,
		SampleLimit:      sampleLimit,
		DryRun:           !execute,
		OK:               true,
	}

	nodes, err := graph.ValidAt(ctx, namespace, time.Now(), nil)
	if err != nil {
		report.OK = false
		report.ValidationErrors = append(report.ValidationErrors, "graph scan: "+err.Error())
		return report, err
	}
	if len(nodes) > sampleLimit {
		nodes = nodes[:sampleLimit]
	}
	report.SampledNodes = len(nodes)

	for _, node := range nodes {
		if len(node.Vector) == 0 {
			continue
		}
		report.VectorNodes++
		results, err := vecs.Search(ctx, store.VectorQuery{
			Namespace: namespace,
			Vector:    node.Vector,
			TopK:      10,
			AsOf:      time.Now(),
		})
		if err != nil {
			report.OK = false
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("vector search failed for %s: %v", node.ID, err))
			continue
		}
		if scoredResultsContainNode(results, node.ID) {
			continue
		}
		report.CandidateIDs = append(report.CandidateIDs, node.ID.String())
		if !execute {
			continue
		}
		if reg, ok := vecs.(interface{ RegisterNode(core.Node) }); ok {
			reg.RegisterNode(node)
		}
		nID := node.ID
		text, _ := node.Properties["text"].(string)
		if err := vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: namespace,
			NodeID:    &nID,
			Vector:    node.Vector,
			Text:      text,
			ModelID:   node.ModelID,
			CreatedAt: time.Now(),
		}); err != nil {
			report.OK = false
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("reindex failed for %s: %v", node.ID, err))
			continue
		}
		report.ReindexedIDs = append(report.ReindexedIDs, node.ID.String())
	}
	return report, nil
}

func scoredResultsContainNode(results []core.ScoredNode, id uuid.UUID) bool {
	for _, result := range results {
		if result.Node.ID == id {
			return true
		}
	}
	return false
}

func readSnapshotLifecycleIndex(path string) (snapshotLifecycleIndex, error) {
	var index snapshotLifecycleIndex
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return index, err
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, fmt.Errorf("decode lifecycle index: %w", err)
	}
	return index, nil
}

func snapshotLifecycleBundleMap(bundles []snapshotLifecycleRetentionBundle) map[string]snapshotLifecycleRetentionBundle {
	out := make(map[string]snapshotLifecycleRetentionBundle, len(bundles))
	for _, bundle := range bundles {
		out[snapshotLifecycleBundleKey(bundle)] = bundle
	}
	return out
}

func snapshotLifecycleBundleKey(bundle snapshotLifecycleRetentionBundle) string {
	parts := []string{
		strings.TrimSpace(bundle.Namespace),
		strings.TrimSpace(bundle.CreatedAt),
		filepath.Base(strings.TrimSpace(bundle.Summary)),
	}
	key := strings.Join(parts, "|")
	if strings.Trim(key, "|") != "" {
		return key
	}
	return strings.TrimSpace(bundle.Summary)
}

func diffSnapshotLifecycleIndexBundle(key string, oldBundle, newBundle snapshotLifecycleRetentionBundle) (snapshotLifecycleIndexBundleDiff, bool) {
	diff := snapshotLifecycleIndexBundleDiff{Bundle: key}
	if oldBundle.Decision != newBundle.Decision {
		diff.DecisionChanged = true
		diff.OldDecision = oldBundle.Decision
		diff.NewDecision = newBundle.Decision
	}
	oldArtifacts := snapshotLifecycleArtifactMap(oldBundle.Artifacts)
	newArtifacts := snapshotLifecycleArtifactMap(newBundle.Artifacts)
	for _, artifactKey := range sortedStringKeys(newArtifacts) {
		newArtifact := newArtifacts[artifactKey]
		oldArtifact, ok := oldArtifacts[artifactKey]
		if !ok {
			diff.ArtifactChanges = append(diff.ArtifactChanges, snapshotLifecycleArtifactChange(newArtifact, "added", snapshotLifecycleRetentionArtifact{}))
			continue
		}
		if oldArtifact.Bytes != newArtifact.Bytes || !strings.EqualFold(oldArtifact.ChecksumSHA256, newArtifact.ChecksumSHA256) {
			diff.ArtifactChanges = append(diff.ArtifactChanges, snapshotLifecycleArtifactChange(newArtifact, "changed", oldArtifact))
		}
	}
	for _, artifactKey := range sortedStringKeys(oldArtifacts) {
		if _, ok := newArtifacts[artifactKey]; !ok {
			diff.ArtifactChanges = append(diff.ArtifactChanges, snapshotLifecycleArtifactChange(oldArtifacts[artifactKey], "removed", snapshotLifecycleRetentionArtifact{}))
		}
	}
	return diff, diff.DecisionChanged || len(diff.ArtifactChanges) > 0
}

func snapshotLifecycleArtifactMap(artifacts []snapshotLifecycleRetentionArtifact) map[string]snapshotLifecycleRetentionArtifact {
	out := make(map[string]snapshotLifecycleRetentionArtifact, len(artifacts))
	for _, artifact := range artifacts {
		out[snapshotLifecycleArtifactKey(artifact)] = artifact
	}
	return out
}

func snapshotLifecycleArtifactKey(artifact snapshotLifecycleRetentionArtifact) string {
	return strings.TrimSpace(artifact.Kind) + "|" + filepath.Base(strings.TrimSpace(artifact.Path))
}

func snapshotLifecycleArtifactChange(artifact snapshotLifecycleRetentionArtifact, change string, oldArtifact snapshotLifecycleRetentionArtifact) snapshotLifecycleIndexArtifactDiff {
	diff := snapshotLifecycleIndexArtifactDiff{
		Kind:      artifact.Kind,
		Path:      filepath.Base(strings.TrimSpace(artifact.Path)),
		Change:    change,
		NewBytes:  artifact.Bytes,
		NewSHA256: artifact.ChecksumSHA256,
	}
	if change == "changed" {
		diff.OldBytes = oldArtifact.Bytes
		diff.OldSHA256 = oldArtifact.ChecksumSHA256
	}
	if change == "removed" {
		diff.OldBytes = artifact.Bytes
		diff.OldSHA256 = artifact.ChecksumSHA256
		diff.NewBytes = 0
		diff.NewSHA256 = ""
	}
	return diff
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func verifySnapshotLifecycleIndex(path string) (snapshotLifecycleIndexVerifyReport, error) {
	path = strings.TrimSpace(path)
	report := snapshotLifecycleIndexVerifyReport{IndexFile: path}
	if path == "" {
		return report, fmt.Errorf("--in is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, fmt.Errorf("read lifecycle index: %w", err)
	}
	var index snapshotLifecycleIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return report, fmt.Errorf("decode lifecycle index: %w", err)
	}
	report.SchemaVersion = index.SchemaVersion
	report.ContextDBVersion = index.ContextDBVersion
	report.TotalBundles = len(index.Bundles)
	if index.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported index schema_version %d", index.SchemaVersion))
	}
	for _, bundle := range index.Bundles {
		for _, artifact := range bundle.Artifacts {
			check := verifySnapshotLifecycleIndexArtifact(artifact)
			report.TotalArtifacts++
			if len(check.Errors) == 0 {
				report.VerifiedArtifacts++
			} else {
				report.ValidationErrors = append(report.ValidationErrors, check.Errors...)
			}
			report.Artifacts = append(report.Artifacts, check)
		}
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("lifecycle index verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func verifySnapshotLifecycleIndexArtifact(artifact snapshotLifecycleRetentionArtifact) snapshotLifecycleIndexArtifactCheck {
	check := snapshotLifecycleIndexArtifactCheck{
		Kind:           artifact.Kind,
		Path:           strings.TrimSpace(artifact.Path),
		ExpectedBytes:  artifact.Bytes,
		ExpectedSHA256: strings.TrimSpace(artifact.ChecksumSHA256),
	}
	if check.Path == "" {
		if artifact.Exists {
			check.Errors = append(check.Errors, "indexed artifact path is empty")
		}
		return check
	}
	info, err := os.Stat(check.Path)
	if err != nil || info.IsDir() {
		if artifact.Exists {
			check.Errors = append(check.Errors, fmt.Sprintf("indexed artifact missing: %s", check.Path))
		}
		return check
	}
	check.Exists = true
	check.ActualBytes = info.Size()
	if artifact.Exists && artifact.Bytes != check.ActualBytes {
		check.Errors = append(check.Errors, fmt.Sprintf("artifact size mismatch for %s: index=%d actual=%d", check.Path, artifact.Bytes, check.ActualBytes))
	}
	if check.ExpectedSHA256 != "" {
		data, err := os.ReadFile(check.Path)
		if err != nil {
			check.Errors = append(check.Errors, fmt.Sprintf("read indexed artifact %s: %v", check.Path, err))
			return check
		}
		sum := sha256.Sum256(data)
		check.ActualSHA256 = hex.EncodeToString(sum[:])
		if !strings.EqualFold(check.ExpectedSHA256, check.ActualSHA256) {
			check.Errors = append(check.Errors, fmt.Sprintf("artifact checksum mismatch for %s", check.Path))
		}
	}
	return check
}

func addSnapshotLifecycleIndexHashes(bundles []snapshotLifecycleRetentionBundle) {
	for i := range bundles {
		for j := range bundles[i].Artifacts {
			if !bundles[i].Artifacts[j].Exists || strings.TrimSpace(bundles[i].Artifacts[j].Path) == "" {
				continue
			}
			data, err := os.ReadFile(bundles[i].Artifacts[j].Path)
			if err != nil {
				continue
			}
			sum := sha256.Sum256(data)
			bundles[i].Artifacts[j].ChecksumSHA256 = hex.EncodeToString(sum[:])
		}
	}
}

func resolveLifecycleSummaryPath(baseDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func lifecycleFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func buildSnapshotArtifactManifest(opts snapshotArtifactManifestOptions) (snapshotArtifactManifest, error) {
	backupPath := strings.TrimSpace(opts.BackupPath)
	if backupPath == "" || backupPath == "-" {
		return snapshotArtifactManifest{}, fmt.Errorf("--manifest requires --out to be a file path")
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return snapshotArtifactManifest{}, fmt.Errorf("read backup for artifact manifest: %w", err)
	}
	counts, err := countSnapshotArtifactRecords(data)
	if err != nil {
		return snapshotArtifactManifest{}, err
	}
	sum := sha256.Sum256(data)
	return snapshotArtifactManifest{
		SchemaVersion:    1,
		Namespace:        opts.Namespace,
		BackupFile:       filepath.Base(backupPath),
		BackupBytes:      int64(len(data)),
		ChecksumSHA256:   hex.EncodeToString(sum[:]),
		CreatedAt:        opts.CreatedAt.UTC().Format(time.RFC3339),
		ContextDBVersion: buildinfo.Version,
		BackupMarker:     strings.TrimSpace(opts.BackupMarker),
		Records:          counts,
	}, nil
}

func verifySnapshotArtifactManifest(manifestPath, backupPath string) (snapshotArtifactVerifyReport, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return snapshotArtifactVerifyReport{}, fmt.Errorf("--manifest is required")
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return snapshotArtifactVerifyReport{}, fmt.Errorf("read artifact manifest: %w", err)
	}
	var manifest snapshotArtifactManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return snapshotArtifactVerifyReport{}, fmt.Errorf("decode artifact manifest: %w", err)
	}
	backupPath = strings.TrimSpace(backupPath)
	if backupPath == "" {
		if strings.TrimSpace(manifest.BackupFile) == "" {
			return snapshotArtifactVerifyReport{}, fmt.Errorf("manifest backup_file is empty; pass --in")
		}
		backupPath = filepath.Join(filepath.Dir(manifestPath), manifest.BackupFile)
	}
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return snapshotArtifactVerifyReport{}, fmt.Errorf("read backup: %w", err)
	}
	counts, err := countSnapshotArtifactRecords(backupData)
	if err != nil {
		return snapshotArtifactVerifyReport{}, err
	}
	sum := sha256.Sum256(backupData)
	actualSHA := hex.EncodeToString(sum[:])
	report := snapshotArtifactVerifyReport{
		Manifest:         manifestPath,
		BackupFile:       backupPath,
		ExpectedBytes:    manifest.BackupBytes,
		ActualBytes:      int64(len(backupData)),
		ExpectedSHA256:   manifest.ChecksumSHA256,
		ActualSHA256:     actualSHA,
		ExpectedRecords:  manifest.Records,
		ActualRecords:    counts,
		ContextDBVersion: buildinfo.Version,
		ManifestVersion:  manifest.ContextDBVersion,
		SchemaVersion:    manifest.SchemaVersion,
	}
	if manifest.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("unsupported schema_version %d", manifest.SchemaVersion))
	}
	if manifest.BackupBytes != report.ActualBytes {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("backup_bytes mismatch: manifest=%d actual=%d", manifest.BackupBytes, report.ActualBytes))
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.ChecksumSHA256), actualSHA) {
		report.ValidationErrors = append(report.ValidationErrors, "checksum_sha256 mismatch")
	}
	if manifest.Records != counts {
		report.ValidationErrors = append(report.ValidationErrors, "record counts mismatch")
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("artifact manifest verification failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func rehearseSnapshotRestore(ctx context.Context, db *client.DB, namespace, manifestPath, backupPath string) (snapshotRehearsalReport, error) {
	verifyReport, err := verifySnapshotArtifactManifest(manifestPath, backupPath)
	report := snapshotRehearsalReport{
		Namespace:       namespace,
		RehearsedAt:     time.Now().UTC().Format(time.RFC3339),
		TargetNamespace: namespace,
		Verification:    verifyReport,
	}
	if err != nil {
		return report, err
	}
	report.RecommendedImportCommand = recommendedSnapshotImportCommand(namespace, verifyReport.BackupFile)
	in, closeIn, err := inputReader(verifyReport.BackupFile)
	if err != nil {
		return report, err
	}
	defer closeIn()
	restoreReport, err := db.ValidateSnapshotReport(ctx, namespace, in)
	report.Restore = restoreReport
	report.OK = err == nil
	if err != nil {
		return report, err
	}
	return report, nil
}

func recommendedSnapshotImportCommand(namespace, backupPath string) string {
	return "contextdb snapshot import --namespace " + shellQuote(namespace) + " --in " + shellQuote(backupPath) + " --report"
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func countSnapshotArtifactRecords(data []byte) (snapshotArtifactCounts, error) {
	var counts snapshotArtifactCounts
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return counts, fmt.Errorf("count artifact manifest record line %d: %w", counts.Lines+1, err)
		}
		counts.Lines++
		switch rec.Type {
		case "node":
			counts.Nodes++
		case "edge":
			counts.Edges++
		case "source":
			counts.Sources++
		default:
			return counts, fmt.Errorf("count artifact manifest record line %d: unknown record type %q", counts.Lines, rec.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return counts, fmt.Errorf("count artifact manifest records: %w", err)
	}
	return counts, nil
}

type nornPorts struct {
	GRPC    int `json:"grpc"`
	REST    int `json:"rest"`
	Observe int `json:"observe"`
}

type nornManifestEntry struct {
	App         string    `json:"app"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Endpoint    string    `json:"endpoint"`
	HealthURL   string    `json:"health_url"`
	GraphQLURL  string    `json:"graphql_url"`
	FeaturesURL string    `json:"features_url"`
	Ports       nornPorts `json:"ports"`
	Tags        []string  `json:"tags,omitempty"`
}

type nornManifestDocument struct {
	Services []nornManifestEntry `json:"services"`
}

type nornDriftReport struct {
	OK       bool               `json:"ok"`
	Expected nornManifestEntry  `json:"expected"`
	Actual   nornManifestEntry  `json:"actual"`
	Diffs    []nornManifestDiff `json:"diffs,omitempty"`
}

type nornPublishReport struct {
	OK               bool              `json:"ok"`
	DryRun           bool              `json:"dry_run"`
	Published        bool              `json:"published"`
	PublishURL       string            `json:"publish_url,omitempty"`
	Method           string            `json:"method,omitempty"`
	Status           string            `json:"status,omitempty"`
	Response         string            `json:"response,omitempty"`
	Entry            nornManifestEntry `json:"entry"`
	ValidationErrors []string          `json:"validation_errors,omitempty"`
}

type nornManifestDiff struct {
	Field    string `json:"field"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

func runNorn(args []string) {
	if len(args) == 0 || args[0] == "manifest" {
		runNornManifest(dropSubcommand(args, "manifest"))
		return
	}
	if args[0] == "validate" {
		runNornValidate(args[1:])
		return
	}
	if args[0] == "drift" {
		runNornDrift(args[1:])
		return
	}
	if args[0] == "publish" {
		runNornPublish(args[1:])
		return
	}
	fmt.Fprintf(os.Stderr, "contextdb norn: unknown subcommand %q\n", args[0])
	os.Exit(2)
}

func runNornManifest(args []string) {
	fs := flag.NewFlagSet("contextdb norn manifest", flag.ExitOnError)
	app := fs.String("app", "contextdb", "Norn app id")
	name := fs.String("name", "contextdb", "Norn service name")
	endpoint := fs.String("endpoint", defaultNornEndpoint(), "public REST endpoint advertised through Norn")
	grpcAddr := fs.String("grpc-addr", getenv("CONTEXTDB_GRPC_ADDR", ":7700"), "gRPC listen address")
	restAddr := fs.String("rest-addr", getenv("CONTEXTDB_REST_ADDR", ":7701"), "REST listen address")
	observeAddr := fs.String("observe-addr", getenv("CONTEXTDB_OBS_ADDR", ":7702"), "observe listen address")
	tags := fs.String("tags", "contextdb,rest,graphql", "comma-separated service tags")
	_ = fs.Parse(args)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         *app,
		Name:        *name,
		Endpoint:    *endpoint,
		GRPCAddr:    *grpcAddr,
		RESTAddr:    *restAddr,
		ObserveAddr: *observeAddr,
		Tags:        splitComma(*tags),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn manifest: %v\n", err)
		os.Exit(2)
	}
	writeIndentedJSON(entry)
}

func runNornValidate(args []string) {
	fs := flag.NewFlagSet("contextdb norn validate", flag.ExitOnError)
	path := fs.String("file", "-", "manifest entry JSON file, or - for stdin")
	_ = fs.Parse(args)

	var data []byte
	var err error
	if *path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(*path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn validate: read manifest: %v\n", err)
		os.Exit(2)
	}
	var entry nornManifestEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn validate: decode manifest: %v\n", err)
		os.Exit(2)
	}
	if err := validateNornManifestEntry(entry); err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn validate: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "ok")
}

func runNornDrift(args []string) {
	fs := flag.NewFlagSet("contextdb norn drift", flag.ExitOnError)
	manifestURL := fs.String("manifest-url", os.Getenv("NORN_MANIFEST_URL"), "Norn manifest URL")
	app := fs.String("app", "contextdb", "Norn app id")
	name := fs.String("name", "contextdb", "Norn service name")
	endpoint := fs.String("endpoint", defaultNornEndpoint(), "public REST endpoint expected in Norn")
	grpcAddr := fs.String("grpc-addr", getenv("CONTEXTDB_GRPC_ADDR", ":7700"), "gRPC listen address")
	restAddr := fs.String("rest-addr", getenv("CONTEXTDB_REST_ADDR", ":7701"), "REST listen address")
	observeAddr := fs.String("observe-addr", getenv("CONTEXTDB_OBS_ADDR", ":7702"), "observe listen address")
	tags := fs.String("tags", "contextdb,rest,graphql", "comma-separated service tags")
	timeout := fs.Duration("timeout", 5*time.Second, "manifest fetch timeout")
	_ = fs.Parse(args)

	if strings.TrimSpace(*manifestURL) == "" {
		fmt.Fprintln(os.Stderr, "contextdb norn drift: --manifest-url or NORN_MANIFEST_URL is required")
		os.Exit(2)
	}
	expected, err := buildNornManifestEntry(nornManifestOptions{
		App:         *app,
		Name:        *name,
		Endpoint:    *endpoint,
		GRPCAddr:    *grpcAddr,
		RESTAddr:    *restAddr,
		ObserveAddr: *observeAddr,
		Tags:        splitComma(*tags),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn drift: expected manifest: %v\n", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	actual, err := fetchNornManifestEntry(ctx, *manifestURL, expected.App, expected.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb norn drift: %v\n", err)
		os.Exit(2)
	}
	report := buildNornDriftReport(expected, actual)
	writeIndentedJSON(report)
	if !report.OK {
		os.Exit(1)
	}
}

func runNornPublish(args []string) {
	fs := flag.NewFlagSet("contextdb norn publish", flag.ExitOnError)
	publishURL := fs.String("publish-url", os.Getenv("NORN_PUBLISH_URL"), "Norn manifest publish endpoint")
	method := fs.String("method", getenv("NORN_PUBLISH_METHOD", http.MethodPost), "HTTP method for publishing")
	token := fs.String("token", os.Getenv("NORN_TOKEN"), "optional bearer token for the publish endpoint")
	app := fs.String("app", "contextdb", "Norn app id")
	name := fs.String("name", "contextdb", "Norn service name")
	endpoint := fs.String("endpoint", defaultNornEndpoint(), "public REST endpoint advertised through Norn")
	grpcAddr := fs.String("grpc-addr", getenv("CONTEXTDB_GRPC_ADDR", ":7700"), "gRPC listen address")
	restAddr := fs.String("rest-addr", getenv("CONTEXTDB_REST_ADDR", ":7701"), "REST listen address")
	observeAddr := fs.String("observe-addr", getenv("CONTEXTDB_OBS_ADDR", ":7702"), "observe listen address")
	tags := fs.String("tags", "contextdb,rest,graphql", "comma-separated service tags")
	dryRunFlag := fs.Bool("dry-run", true, "validate and print the publish plan without sending it")
	execute := fs.Bool("execute", false, "send the manifest to --publish-url")
	reportOut := fs.Bool("report", false, "print a JSON publish report")
	timeout := fs.Duration("timeout", 5*time.Second, "publish request timeout")
	_ = fs.Parse(args)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         *app,
		Name:        *name,
		Endpoint:    *endpoint,
		GRPCAddr:    *grpcAddr,
		RESTAddr:    *restAddr,
		ObserveAddr: *observeAddr,
		Tags:        splitComma(*tags),
	})
	if err != nil {
		report := nornPublishReport{DryRun: !*execute && *dryRunFlag, PublishURL: strings.TrimSpace(*publishURL), Method: strings.ToUpper(strings.TrimSpace(*method))}
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		if *reportOut {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb norn publish: %v\n", err)
		os.Exit(2)
	}

	dryRun := *dryRunFlag && !*execute
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := buildNornPublishReport(ctx, http.DefaultClient, entry, nornPublishOptions{
		PublishURL: *publishURL,
		Method:     *method,
		Token:      *token,
		DryRun:     dryRun,
	})
	if err != nil {
		if *reportOut {
			writeIndentedJSON(report)
		}
		fmt.Fprintf(os.Stderr, "contextdb norn publish: %v\n", err)
		os.Exit(1)
	}
	if *reportOut {
		writeIndentedJSON(report)
	} else if report.DryRun {
		fmt.Fprintln(os.Stdout, "dry-run ok")
	} else {
		fmt.Fprintln(os.Stdout, "published")
	}
}

type nornManifestOptions struct {
	App         string
	Name        string
	Endpoint    string
	GRPCAddr    string
	RESTAddr    string
	ObserveAddr string
	Tags        []string
}

type nornPublishOptions struct {
	PublishURL string
	Method     string
	Token      string
	DryRun     bool
}

func buildNornManifestEntry(opts nornManifestOptions) (nornManifestEntry, error) {
	endpoint, err := normalizeEndpoint(opts.Endpoint)
	if err != nil {
		return nornManifestEntry{}, err
	}
	entry := nornManifestEntry{
		App:         strings.TrimSpace(opts.App),
		Name:        strings.TrimSpace(opts.Name),
		Version:     buildinfo.Version,
		Endpoint:    endpoint,
		HealthURL:   endpoint + "/v1/ping",
		GraphQLURL:  endpoint + "/graphql",
		FeaturesURL: endpoint + "/v1/features",
		Ports: nornPorts{
			GRPC:    portFromAddr(opts.GRPCAddr, 7700),
			REST:    portFromAddr(opts.RESTAddr, 7701),
			Observe: portFromAddr(opts.ObserveAddr, 7702),
		},
		Tags: opts.Tags,
	}
	if err := validateNornManifestEntry(entry); err != nil {
		return nornManifestEntry{}, err
	}
	return entry, nil
}

func buildNornPublishReport(ctx context.Context, client *http.Client, entry nornManifestEntry, opts nornPublishOptions) (nornPublishReport, error) {
	report := nornPublishReport{
		DryRun:     opts.DryRun,
		PublishURL: strings.TrimSpace(opts.PublishURL),
		Method:     strings.ToUpper(strings.TrimSpace(opts.Method)),
		Entry:      entry,
	}
	if report.Method == "" {
		report.Method = http.MethodPost
	}
	if err := validateNornManifestEntry(entry); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, err
	}
	if opts.DryRun {
		report.OK = true
		return report, nil
	}
	if report.PublishURL == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--publish-url or NORN_PUBLISH_URL is required when --execute is set")
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	status, response, err := publishNornManifestEntry(ctx, client, report.PublishURL, report.Method, strings.TrimSpace(opts.Token), entry)
	report.Status = status
	report.Response = response
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, err.Error())
		return report, err
	}
	report.OK = true
	report.Published = true
	return report, nil
}

func publishNornManifestEntry(ctx context.Context, client *http.Client, publishURL, method, token string, entry nornManifestEntry) (string, string, error) {
	return publishJSON(ctx, client, publishURL, method, token, entry)
}

func publishJSON(ctx context.Context, client *http.Client, publishURL, method, token string, payload any) (string, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encode publish payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, publishURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("build publish request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("publish manifest: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.Status, "", fmt.Errorf("read publish response: %w", err)
	}
	response := strings.TrimSpace(string(data))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Status, response, fmt.Errorf("publish manifest: unexpected status %s", resp.Status)
	}
	return resp.Status, response, nil
}

func fetchNornManifestEntry(ctx context.Context, manifestURL, app, name string) (nornManifestEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nornManifestEntry{}, fmt.Errorf("build manifest request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nornManifestEntry{}, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nornManifestEntry{}, fmt.Errorf("fetch manifest: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nornManifestEntry{}, fmt.Errorf("read manifest: %w", err)
	}
	entries, err := decodeNornManifestEntries(data)
	if err != nil {
		return nornManifestEntry{}, err
	}
	for _, entry := range entries {
		if entry.App == app && entry.Name == name {
			return entry, nil
		}
	}
	return nornManifestEntry{}, fmt.Errorf("manifest entry app=%q name=%q not found", app, name)
}

func decodeNornManifestEntries(data []byte) ([]nornManifestEntry, error) {
	var document nornManifestDocument
	if err := json.Unmarshal(data, &document); err == nil && len(document.Services) > 0 {
		return document.Services, nil
	}
	var entries []nornManifestEntry
	if err := json.Unmarshal(data, &entries); err == nil && len(entries) > 0 {
		return entries, nil
	}
	var entry nornManifestEntry
	if err := json.Unmarshal(data, &entry); err == nil && entry.App != "" {
		return []nornManifestEntry{entry}, nil
	}
	return nil, fmt.Errorf("decode manifest: expected service object, service array, or object with services")
}

func buildNornDriftReport(expected, actual nornManifestEntry) nornDriftReport {
	diffs := nornManifestDiffs(expected, actual)
	return nornDriftReport{
		OK:       len(diffs) == 0,
		Expected: expected,
		Actual:   actual,
		Diffs:    diffs,
	}
}

func nornManifestDiffs(expected, actual nornManifestEntry) []nornManifestDiff {
	checks := []struct {
		field    string
		expected any
		actual   any
	}{
		{"app", expected.App, actual.App},
		{"name", expected.Name, actual.Name},
		{"version", expected.Version, actual.Version},
		{"endpoint", expected.Endpoint, strings.TrimRight(actual.Endpoint, "/")},
		{"health_url", expected.HealthURL, strings.TrimRight(actual.HealthURL, "/")},
		{"graphql_url", expected.GraphQLURL, strings.TrimRight(actual.GraphQLURL, "/")},
		{"features_url", expected.FeaturesURL, strings.TrimRight(actual.FeaturesURL, "/")},
		{"ports.grpc", expected.Ports.GRPC, actual.Ports.GRPC},
		{"ports.rest", expected.Ports.REST, actual.Ports.REST},
		{"ports.observe", expected.Ports.Observe, actual.Ports.Observe},
		{"tags", expected.Tags, actual.Tags},
	}
	diffs := make([]nornManifestDiff, 0)
	for _, check := range checks {
		if !reflect.DeepEqual(check.expected, check.actual) {
			diffs = append(diffs, nornManifestDiff{
				Field:    check.field,
				Expected: check.expected,
				Actual:   check.actual,
			})
		}
	}
	return diffs
}

func defaultNornEndpoint() string {
	if publicURL := os.Getenv("CONTEXTDB_PUBLIC_URL"); publicURL != "" {
		return publicURL
	}
	restAddr := getenv("CONTEXTDB_REST_ADDR", ":7701")
	if strings.HasPrefix(restAddr, ":") {
		return "http://127.0.0.1" + restAddr
	}
	if strings.HasPrefix(restAddr, "http://") || strings.HasPrefix(restAddr, "https://") {
		return restAddr
	}
	return "http://" + restAddr
}

func validateNornManifestEntry(entry nornManifestEntry) error {
	if strings.TrimSpace(entry.App) != "contextdb" {
		return fmt.Errorf("app must be contextdb")
	}
	if strings.TrimSpace(entry.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if _, err := normalizeEndpoint(entry.Endpoint); err != nil {
		return err
	}
	if entry.Ports.REST <= 0 {
		return fmt.Errorf("ports.rest is required")
	}
	return nil
}

func normalizeEndpoint(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", fmt.Errorf("endpoint is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("endpoint must be an absolute URL")
	}
	return u.String(), nil
}

func portFromAddr(addr string, fallback int) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fallback
	}
	idx := strings.LastIndex(addr, ":")
	if idx < 0 || idx == len(addr)-1 {
		return fallback
	}
	port, err := strconv.Atoi(addr[idx+1:])
	if err != nil || port <= 0 {
		return fallback
	}
	return port
}

func dropSubcommand(args []string, subcommand string) []string {
	if len(args) > 0 && args[0] == subcommand {
		return args[1:]
	}
	return args
}

func writeIndentedJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "contextdb: encode json: %v\n", err)
		os.Exit(2)
	}
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeTextFile(path, value string) error {
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("contextdb doctor", flag.ExitOnError)
	baseURL := fs.String("url", getenv("CONTEXTDB_REST_URL", "http://127.0.0.1:7701"), "contextdb REST base URL")
	sampleWrite := fs.Bool("sample-write", false, "write and retrieve a sample probe node")
	sampleNamespace := fs.String("sample-namespace", "_doctor", "namespace to use with --sample-write")
	backupMarker := fs.String("backup-marker", "", "path to a backup marker file to check for recency")
	maxBackupAge := fs.Duration("max-backup-age", 24*time.Hour, "maximum acceptable age for --backup-marker")
	publishedBackupURL := fs.String("published-backup-url", os.Getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL"), "published backup index metadata URL to check for freshness")
	publishedBackupIndex := fs.String("published-backup-index", "", "local lifecycle index path to compare against published backup metadata")
	publishedBackupMethod := fs.String("published-backup-method", getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_METHOD", http.MethodGet), "HTTP method for fetching published backup metadata")
	publishedBackupToken := fs.String("published-backup-token", os.Getenv("NORN_TOKEN"), "optional bearer token for the published backup metadata endpoint")
	maxPublishedBackupAge := fs.Duration("max-published-backup-age", 24*time.Hour, "maximum acceptable age for --published-backup-url")
	publishedBackupTimeout := fs.Duration("published-backup-timeout", 5*time.Second, "published backup metadata request timeout")
	storeConsistency := fs.Bool("store-consistency", false, "check local graph, vector, and fingerprint consistency")
	storeNamespace := fs.String("store-namespace", "default", "namespace to check with --store-consistency")
	storeSample := fs.Int("store-sample", 100, "maximum valid graph nodes to sample with --store-consistency")
	var kvKeys repeatedStringFlag
	fs.Var(&kvKeys, "kv-key", "expected KV hot key to check; repeat for multiple keys")
	var kvDerivedKeys repeatedStringFlag
	fs.Var(&kvDerivedKeys, "kv-derived-key", "expected derived KV hot key to check for generated_at freshness; repeat for multiple keys")
	maxKVDerivedAge := fs.Duration("max-kv-derived-age", 24*time.Hour, "maximum acceptable generated_at age for --kv-derived-key")
	_ = fs.Parse(args)

	report, err := doctor.Run(context.Background(), doctor.Options{
		BaseURL:         *baseURL,
		SampleWrite:     *sampleWrite,
		SampleNamespace: *sampleNamespace,
		BackupMarker:    *backupMarker,
		MaxBackupAge:    *maxBackupAge,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb doctor: %v\n", err)
		os.Exit(2)
	}
	if strings.TrimSpace(*publishedBackupURL) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), *publishedBackupTimeout)
		defer cancel()
		report.Checks = append(report.Checks, buildPublishedBackupFreshnessCheck(ctx, http.DefaultClient, snapshotLifecycleIndexPublishFreshnessOptions{
			PublishedURL: *publishedBackupURL,
			Method:       *publishedBackupMethod,
			Token:        *publishedBackupToken,
			MaxAge:       *maxPublishedBackupAge,
			Now:          time.Now(),
		}))
		recomputeDoctorReportOK(&report)
	}
	if strings.TrimSpace(*publishedBackupIndex) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), *publishedBackupTimeout)
		defer cancel()
		report.Checks = append(report.Checks, buildPublishedBackupDriftCheck(ctx, http.DefaultClient, *publishedBackupIndex, snapshotLifecycleIndexPublishDriftOptions{
			PublishedURL: *publishedBackupURL,
			Method:       *publishedBackupMethod,
			Token:        *publishedBackupToken,
		}))
		recomputeDoctorReportOK(&report)
	}
	if *storeConsistency || len(kvKeys) > 0 || len(kvDerivedKeys) > 0 {
		db := openSnapshotDB()
		defer db.Close()
		graph, vecs, kv, _ := db.Stores()
		if *storeConsistency {
			report.Checks = append(report.Checks, buildStoreConsistencyCheck(context.Background(), graph, vecs, kv, *storeNamespace, *storeSample))
		}
		if len(kvKeys) > 0 {
			report.Checks = append(report.Checks, buildKVConsistencyCheck(context.Background(), kv, kvKeys))
		}
		if len(kvDerivedKeys) > 0 {
			report.Checks = append(report.Checks, buildKVDerivedFreshnessCheck(context.Background(), kv, kvDerivedKeys, *maxKVDerivedAge, time.Now()))
		}
		recomputeDoctorReportOK(&report)
	}
	writeIndentedJSON(report)
	if !report.OK {
		os.Exit(1)
	}
}

func recomputeDoctorReportOK(report *doctor.Report) {
	report.OK = true
	for _, check := range report.Checks {
		if !check.OK {
			report.OK = false
			return
		}
	}
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	for _, part := range splitComma(value) {
		*f = append(*f, part)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// splitComma splits a comma-separated string into a slice of non-empty trimmed values.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
