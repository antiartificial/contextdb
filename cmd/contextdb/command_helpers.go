package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/doctor"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

func publishBundleKey(bundle snapshotLifecycleIndexPublishBundleSummary) string {
	return bundle.Namespace + "\x00" + bundle.CreatedAt + "\x00" + bundle.Summary
}

func bundleIndexValidationPrefix(item rankingEvalBaselineArtifactManifestVerifyBundleIndexVerifyReportItem, msg string) string {
	label := strings.TrimSpace(item.Kind)
	if label == "" {
		label = "artifact"
	}
	if strings.TrimSpace(item.Path) != "" {
		return fmt.Sprintf("%s %s: %s", label, item.Path, msg)
	}
	return fmt.Sprintf("%s: %s", label, msg)
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

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func markdownInline(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}

func ciAnnotationEscape(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	value = strings.ReplaceAll(value, "\n", "%0A")
	value = strings.ReplaceAll(value, ":", "%3A")
	value = strings.ReplaceAll(value, ",", "%2C")
	return value
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
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

func isLowerHexSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
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

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

func parseEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
