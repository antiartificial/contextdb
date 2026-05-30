package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Options struct {
	BaseURL         string
	Client          *http.Client
	SampleWrite     bool
	SampleNamespace string
	BackupMarker    string
	MaxBackupAge    time.Duration
}

type Report struct {
	Target          string        `json:"target"`
	OK              bool          `json:"ok"`
	Version         string        `json:"version,omitempty"`
	APIVersion      string        `json:"api_version,omitempty"`
	LatestMigration int           `json:"latest_migration,omitempty"`
	SampleNodeID    string        `json:"sample_node_id,omitempty"`
	Checks          []CheckResult `json:"checks"`
}

type CheckResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type versionResponse struct {
	Version         string          `json:"version"`
	APIVersion      string          `json:"api_version"`
	LatestMigration int             `json:"latest_migration"`
	Features        []featureInfo   `json:"features"`
	Migrations      []migrationInfo `json:"migrations"`
}

type featuresResponse struct {
	Version  string        `json:"version"`
	Features []featureInfo `json:"features"`
}

type migrationsResponse struct {
	Version         string          `json:"version"`
	LatestMigration int             `json:"latest_migration"`
	Migrations      []migrationInfo `json:"migrations"`
}

type featureInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Since  string `json:"since"`
}

type migrationInfo struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

type writeRequest struct {
	Mode       string    `json:"mode"`
	Content    string    `json:"content"`
	SourceID   string    `json:"source_id"`
	Labels     []string  `json:"labels"`
	Vector     []float32 `json:"vector"`
	Confidence float64   `json:"confidence"`
	Dedup      bool      `json:"dedup"`
}

type writeResponse struct {
	NodeID   string `json:"node_id"`
	Admitted bool   `json:"admitted"`
	Reason   string `json:"reason,omitempty"`
}

type retrieveRequest struct {
	Vector []float32 `json:"vector"`
	TopK   int       `json:"top_k"`
	Labels []string  `json:"labels,omitempty"`
}

type retrieveResponse struct {
	Results []struct {
		ID              string  `json:"id"`
		Score           float64 `json:"score"`
		SimilarityScore float64 `json:"similarity_score"`
	} `json:"results"`
}

func Run(ctx context.Context, opts Options) (Report, error) {
	target, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return Report{}, err
	}
	httpClient := opts.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	runner := &runner{baseURL: target, client: httpClient}
	report := Report{Target: target}
	report.Checks = append(report.Checks, runner.checkPing(ctx))

	version, check := runner.checkVersion(ctx)
	report.Checks = append(report.Checks, check)
	report.Version = version.Version
	report.APIVersion = version.APIVersion
	report.LatestMigration = version.LatestMigration

	report.Checks = append(report.Checks, runner.checkFeatures(ctx))
	report.Checks = append(report.Checks, runner.checkMigrations(ctx))
	if opts.SampleWrite {
		namespace := strings.TrimSpace(opts.SampleNamespace)
		if namespace == "" {
			namespace = "_doctor"
		}
		nodeID, check := runner.checkSampleWrite(ctx, namespace)
		report.Checks = append(report.Checks, check)
		report.SampleNodeID = nodeID
	}
	if strings.TrimSpace(opts.BackupMarker) != "" {
		report.Checks = append(report.Checks, checkBackupMarker(opts.BackupMarker, opts.MaxBackupAge))
	}

	report.OK = true
	for _, check := range report.Checks {
		if !check.OK {
			report.OK = false
			break
		}
	}
	return report, nil
}

type runner struct {
	baseURL string
	client  *http.Client
}

func (r *runner) checkPing(ctx context.Context) CheckResult {
	var body struct {
		Status string `json:"status"`
	}
	if err := r.getJSON(ctx, "/v1/ping", &body); err != nil {
		return CheckResult{Name: "ping", OK: false, Detail: err.Error()}
	}
	if body.Status != "ok" {
		return CheckResult{Name: "ping", OK: false, Detail: "unexpected status " + body.Status}
	}
	return CheckResult{Name: "ping", OK: true, Detail: "server responded"}
}

func (r *runner) checkVersion(ctx context.Context) (versionResponse, CheckResult) {
	var body versionResponse
	if err := r.getJSON(ctx, "/v1/version", &body); err != nil {
		return body, CheckResult{Name: "version", OK: false, Detail: err.Error()}
	}
	if strings.TrimSpace(body.Version) == "" {
		return body, CheckResult{Name: "version", OK: false, Detail: "missing version"}
	}
	if strings.TrimSpace(body.APIVersion) == "" {
		return body, CheckResult{Name: "version", OK: false, Detail: "missing api_version"}
	}
	return body, CheckResult{Name: "version", OK: true, Detail: body.Version + " / " + body.APIVersion}
}

func (r *runner) checkFeatures(ctx context.Context) CheckResult {
	var body featuresResponse
	if err := r.getJSON(ctx, "/v1/features", &body); err != nil {
		return CheckResult{Name: "features", OK: false, Detail: err.Error()}
	}
	if len(body.Features) == 0 {
		return CheckResult{Name: "features", OK: false, Detail: "no features returned"}
	}
	for _, feature := range body.Features {
		if feature.Name == "feature-introspection" {
			return CheckResult{Name: "features", OK: true, Detail: fmt.Sprintf("%d features", len(body.Features))}
		}
	}
	return CheckResult{Name: "features", OK: false, Detail: "feature-introspection not advertised"}
}

func (r *runner) checkMigrations(ctx context.Context) CheckResult {
	var body migrationsResponse
	if err := r.getJSON(ctx, "/v1/migrations", &body); err != nil {
		return CheckResult{Name: "migrations", OK: false, Detail: err.Error()}
	}
	if len(body.Migrations) == 0 {
		return CheckResult{Name: "migrations", OK: false, Detail: "no migrations returned"}
	}
	if body.LatestMigration < body.Migrations[len(body.Migrations)-1].Version {
		return CheckResult{Name: "migrations", OK: false, Detail: "latest migration is behind advertised migrations"}
	}
	return CheckResult{Name: "migrations", OK: true, Detail: fmt.Sprintf("latest=%d count=%d", body.LatestMigration, len(body.Migrations))}
}

func (r *runner) checkSampleWrite(ctx context.Context, namespace string) (string, CheckResult) {
	vector := []float32{1, 0, 0, 0}
	writeReq := writeRequest{
		Mode:       "general",
		Content:    "contextdb doctor sample write retrieve probe",
		SourceID:   "contextdb-doctor",
		Labels:     []string{"DoctorProbe"},
		Vector:     vector,
		Confidence: 0.99,
		Dedup:      true,
	}
	var writeResp writeResponse
	if err := r.postJSON(ctx, "/v1/namespaces/"+url.PathEscape(namespace)+"/write", writeReq, &writeResp); err != nil {
		return "", CheckResult{Name: "sample_write", OK: false, Detail: err.Error()}
	}
	if !writeResp.Admitted {
		return writeResp.NodeID, CheckResult{Name: "sample_write", OK: false, Detail: "write was not admitted: " + writeResp.Reason}
	}
	if strings.TrimSpace(writeResp.NodeID) == "" {
		return "", CheckResult{Name: "sample_write", OK: false, Detail: "write response missing node_id"}
	}

	var retrieveResp retrieveResponse
	if err := r.postJSON(ctx, "/v1/namespaces/"+url.PathEscape(namespace)+"/retrieve", retrieveRequest{
		Vector: vector,
		TopK:   5,
		Labels: []string{"DoctorProbe"},
	}, &retrieveResp); err != nil {
		return writeResp.NodeID, CheckResult{Name: "sample_write", OK: false, Detail: "retrieve: " + err.Error()}
	}
	for _, result := range retrieveResp.Results {
		if result.ID == writeResp.NodeID {
			return writeResp.NodeID, CheckResult{Name: "sample_write", OK: true, Detail: "wrote and retrieved " + writeResp.NodeID}
		}
	}
	return writeResp.NodeID, CheckResult{Name: "sample_write", OK: false, Detail: "written node not found in retrieve results"}
}

func checkBackupMarker(path string, maxAge time.Duration) CheckResult {
	info, err := os.Stat(path)
	if err != nil {
		return CheckResult{Name: "backup_readiness", OK: false, Detail: err.Error()}
	}
	if info.IsDir() {
		return CheckResult{Name: "backup_readiness", OK: false, Detail: "backup marker is a directory"}
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	age := time.Since(info.ModTime())
	if age < 0 {
		age = 0
	}
	if age > maxAge {
		return CheckResult{Name: "backup_readiness", OK: false, Detail: fmt.Sprintf("marker age %s exceeds %s", age.Round(time.Second), maxAge)}
	}
	return CheckResult{Name: "backup_readiness", OK: true, Detail: fmt.Sprintf("marker age %s within %s", age.Round(time.Second), maxAge)}
}

func (r *runner) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s decode: %w", path, err)
	}
	return nil
}

func (r *runner) postJSON(ctx context.Context, path string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s decode: %w", path, err)
	}
	return nil
}

func normalizeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "http://127.0.0.1:7701"
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}
