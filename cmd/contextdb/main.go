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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

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
