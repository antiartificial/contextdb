package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/antiartificial/contextdb/internal/buildinfo"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"
)

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
