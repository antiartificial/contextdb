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
	"github.com/antiartificial/contextdb/internal/doctor"
	"github.com/antiartificial/contextdb/internal/store"
	"os"
	"sort"
	"strings"
	"time"
)

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
	if len(args) > 0 && args[0] == "receipt" {
		runRepairKVCacheReceipt(args[1:])
		return
	}
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
	receiptOut := fs.String("receipt-out", "", "write a JSON derived KV refresh receipt after successful --execute")
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
	if strings.TrimSpace(*receiptOut) != "" {
		if !*execute {
			fmt.Fprintln(os.Stderr, "contextdb repair kv-cache: --receipt-out requires --execute")
			os.Exit(2)
		}
		if valueSource != "derived:recent-nodes" {
			fmt.Fprintln(os.Stderr, "contextdb repair kv-cache: --receipt-out requires --derive recent-nodes")
			os.Exit(2)
		}
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
	if strings.TrimSpace(*receiptOut) != "" {
		receipt, receiptErr := buildKVRefreshReceipt(report, valueBytes)
		if receiptErr != nil {
			fmt.Fprintf(os.Stderr, "contextdb repair kv-cache: %v\n", receiptErr)
			os.Exit(1)
		}
		if writeErr := writeJSONFile(*receiptOut, receipt); writeErr != nil {
			fmt.Fprintf(os.Stderr, "contextdb repair kv-cache: %v\n", writeErr)
			os.Exit(1)
		}
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

func runRepairKVCacheReceipt(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb repair kv-cache receipt: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runRepairKVCacheReceiptVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb repair kv-cache receipt: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runRepairKVCacheReceiptVerify(args []string) {
	fs := flag.NewFlagSet("contextdb repair kv-cache receipt verify", flag.ExitOnError)
	receiptPath := fs.String("receipt", "", "JSON derived KV refresh receipt to verify")
	valueFile := fs.String("value-file", "", "optional reviewed derived value file to hash and compare with receipt")
	reportOut := fs.Bool("report", false, "print a JSON derived KV refresh receipt verification report")
	_ = fs.Parse(args)

	report, err := verifyKVRefreshReceipt(*receiptPath, *valueFile)
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb repair kv-cache receipt verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
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

type kvRefreshReceipt struct {
	Kind                     string          `json:"kind"`
	SchemaVersion            int             `json:"schema_version"`
	GeneratedAt              string          `json:"generated_at"`
	ContextDBVersion         string          `json:"contextdb_version"`
	ValueSource              string          `json:"value_source"`
	ValueSHA256              string          `json:"value_sha256"`
	RecommendedDoctorCommand string          `json:"recommended_doctor_command,omitempty"`
	Report                   kvRefreshReport `json:"report"`
}

type kvRefreshReceiptVerifyReport struct {
	OK                       bool     `json:"ok"`
	ReceiptFile              string   `json:"receipt_file"`
	ValueFile                string   `json:"value_file,omitempty"`
	Kind                     string   `json:"kind,omitempty"`
	SchemaVersion            int      `json:"schema_version,omitempty"`
	ContextDBVersion         string   `json:"contextdb_version,omitempty"`
	ValueSource              string   `json:"value_source,omitempty"`
	StoredValueSHA256        string   `json:"stored_value_sha256,omitempty"`
	ComputedValueSHA256      string   `json:"computed_value_sha256,omitempty"`
	ComputedReportSHA256     string   `json:"computed_report_sha256,omitempty"`
	RecommendedDoctorCommand string   `json:"recommended_doctor_command,omitempty"`
	ComputedDoctorCommand    string   `json:"computed_doctor_command,omitempty"`
	WrittenKeys              []string `json:"written_keys,omitempty"`
	ValidationErrors         []string `json:"validation_errors,omitempty"`
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

func buildKVRefreshReceipt(report kvRefreshReport, value []byte) (kvRefreshReceipt, error) {
	if !report.Execute || report.DryRun {
		return kvRefreshReceipt{}, errors.New("kv refresh receipt requires executed report")
	}
	if report.ValueSource != "derived:recent-nodes" {
		return kvRefreshReceipt{}, errors.New("kv refresh receipt requires derived recent-nodes value source")
	}
	sum := sha256.Sum256(value)
	return kvRefreshReceipt{
		Kind:                     "contextdb.kv.refresh.receipt",
		SchemaVersion:            1,
		GeneratedAt:              report.GeneratedAt,
		ContextDBVersion:         buildinfo.Version,
		ValueSource:              report.ValueSource,
		ValueSHA256:              hex.EncodeToString(sum[:]),
		RecommendedDoctorCommand: kvRefreshReceiptDoctorCommand(report),
		Report:                   report,
	}, nil
}

func verifyKVRefreshReceipt(receiptPath, valuePath string) (kvRefreshReceiptVerifyReport, error) {
	report := kvRefreshReceiptVerifyReport{
		ReceiptFile: strings.TrimSpace(receiptPath),
		ValueFile:   strings.TrimSpace(valuePath),
	}
	if report.ReceiptFile == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--receipt is required")
		report.OK = false
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	data, err := os.ReadFile(report.ReceiptFile)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read receipt: %v", err))
		report.OK = false
		return report, err
	}
	var receipt kvRefreshReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("decode receipt: %v", err))
		report.OK = false
		return report, err
	}

	report.Kind = receipt.Kind
	report.SchemaVersion = receipt.SchemaVersion
	report.ContextDBVersion = receipt.ContextDBVersion
	report.ValueSource = receipt.ValueSource
	report.StoredValueSHA256 = receipt.ValueSHA256
	report.RecommendedDoctorCommand = receipt.RecommendedDoctorCommand
	report.ComputedDoctorCommand = kvRefreshReceiptDoctorCommand(receipt.Report)
	report.WrittenKeys = kvRefreshReceiptWrittenKeys(receipt.Report)

	reportData, err := json.Marshal(receipt.Report)
	if err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("marshal embedded report: %v", err))
	} else {
		sum := sha256.Sum256(reportData)
		report.ComputedReportSHA256 = hex.EncodeToString(sum[:])
	}

	if receipt.Kind != "contextdb.kv.refresh.receipt" {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("kind = %q, want contextdb.kv.refresh.receipt", receipt.Kind))
	}
	if receipt.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("schema_version = %d, want 1", receipt.SchemaVersion))
	}
	if receipt.ValueSource != "derived:recent-nodes" {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("value_source = %q, want derived:recent-nodes", receipt.ValueSource))
	}
	if !receipt.Report.Execute || receipt.Report.DryRun {
		report.ValidationErrors = append(report.ValidationErrors, "embedded report must be executed")
	}
	if !receipt.Report.OK {
		report.ValidationErrors = append(report.ValidationErrors, "embedded report ok must be true")
	}
	if receipt.Report.ValueSource != receipt.ValueSource {
		report.ValidationErrors = append(report.ValidationErrors, "receipt value_source does not match embedded report value_source")
	}
	if receipt.GeneratedAt != receipt.Report.GeneratedAt {
		report.ValidationErrors = append(report.ValidationErrors, "receipt generated_at does not match embedded report generated_at")
	}
	if len(report.WrittenKeys) == 0 {
		report.ValidationErrors = append(report.ValidationErrors, "embedded report has no written keys")
	}
	if !isLowerHexSHA256(receipt.ValueSHA256) {
		report.ValidationErrors = append(report.ValidationErrors, "value_sha256 must be a lowercase SHA-256 hex digest")
	}
	if receipt.RecommendedDoctorCommand != report.ComputedDoctorCommand {
		report.ValidationErrors = append(report.ValidationErrors, "recommended_doctor_command does not match embedded report")
	}
	if report.ValueFile != "" {
		value, err := os.ReadFile(report.ValueFile)
		if err != nil {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read value file: %v", err))
		} else {
			sum := sha256.Sum256(value)
			report.ComputedValueSHA256 = hex.EncodeToString(sum[:])
			if receipt.ValueSHA256 != report.ComputedValueSHA256 {
				report.ValidationErrors = append(report.ValidationErrors, "value_sha256 does not match --value-file")
			}
		}
	}

	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func kvRefreshReceiptWrittenKeys(report kvRefreshReport) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range report.Items {
		if item.Action != "written" {
			continue
		}
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
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

func buildKVRefreshReceiptVerifyCheck(receiptPath, valuePath string) doctor.CheckResult {
	report, err := verifyKVRefreshReceipt(receiptPath, valuePath)
	detail := fmt.Sprintf("receipt=%s value_file=%s value_sha256=%s written_keys=%d doctor_command=%s",
		report.ReceiptFile,
		report.ValueFile,
		report.StoredValueSHA256,
		len(report.WrittenKeys),
		report.ComputedDoctorCommand)
	if err != nil {
		if len(report.ValidationErrors) > 0 {
			detail += ": " + strings.Join(report.ValidationErrors, "; ")
		} else {
			detail += ": " + err.Error()
		}
		return doctor.CheckResult{Name: "kv_refresh_receipt_verify", OK: false, Detail: strings.TrimSpace(detail)}
	}
	return doctor.CheckResult{Name: "kv_refresh_receipt_verify", OK: true, Detail: strings.TrimSpace(detail)}
}
