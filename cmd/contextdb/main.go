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
	"flag"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/antiartificial/contextdb/internal/doctor"
	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
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
		fmt.Fprintln(os.Stderr, "contextdb snapshot lifecycle: expected verify or retention")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runSnapshotLifecycleVerify(args[1:])
	case "retention":
		runSnapshotLifecycleRetention(args[1:])
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

func runSnapshotLifecycleRetention(args []string) {
	fs := flag.NewFlagSet("contextdb snapshot lifecycle retention", flag.ExitOnError)
	dir := fs.String("dir", "", "directory containing lifecycle summary files")
	namespace := fs.String("namespace", "", "optional namespace filter")
	keep := fs.Int("keep", 14, "number of newest lifecycle bundles to keep")
	reportOut := fs.Bool("report", false, "print a JSON retention report")
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
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes,omitempty"`
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
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, fmt.Errorf("lifecycle retention report failed: %s", strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
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

type nornManifestOptions struct {
	App         string
	Name        string
	Endpoint    string
	GRPCAddr    string
	RESTAddr    string
	ObserveAddr string
	Tags        []string
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

func runDoctor(args []string) {
	fs := flag.NewFlagSet("contextdb doctor", flag.ExitOnError)
	baseURL := fs.String("url", getenv("CONTEXTDB_REST_URL", "http://127.0.0.1:7701"), "contextdb REST base URL")
	sampleWrite := fs.Bool("sample-write", false, "write and retrieve a sample probe node")
	sampleNamespace := fs.String("sample-namespace", "_doctor", "namespace to use with --sample-write")
	backupMarker := fs.String("backup-marker", "", "path to a backup marker file to check for recency")
	maxBackupAge := fs.Duration("max-backup-age", 24*time.Hour, "maximum acceptable age for --backup-marker")
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
	writeIndentedJSON(report)
	if !report.OK {
		os.Exit(1)
	}
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
