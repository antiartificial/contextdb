package doctor_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/doctor"
)

func TestRun_OK(t *testing.T) {
	is := is.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ping":
			writeJSON(t, w, map[string]string{"status": "ok"})
		case "/v1/version":
			writeJSON(t, w, map[string]any{
				"version":          "0.4.0",
				"api_version":      "v1",
				"latest_migration": 2,
				"features": []map[string]string{
					{"name": "feature-introspection", "status": "stable", "since": "v0.4.0"},
				},
				"migrations": []map[string]any{
					{"version": 1, "name": "initial"},
					{"version": 2, "name": "node_fingerprints"},
				},
			})
		case "/v1/features":
			writeJSON(t, w, map[string]any{
				"version": "0.4.0",
				"features": []map[string]string{
					{"name": "feature-introspection", "status": "stable", "since": "v0.4.0"},
				},
			})
		case "/v1/migrations":
			writeJSON(t, w, map[string]any{
				"version":          "0.4.0",
				"latest_migration": 2,
				"migrations": []map[string]any{
					{"version": 1, "name": "initial"},
					{"version": 2, "name": "node_fingerprints"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report, err := doctor.Run(context.Background(), doctor.Options{BaseURL: srv.URL})
	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.Version, "0.4.0")
	is.Equal(report.APIVersion, "v1")
	is.Equal(report.LatestMigration, 2)
	is.Equal(len(report.Checks), 4)
}

func TestRun_SampleWrite(t *testing.T) {
	is := is.New(t)
	const nodeID = "550e8400-e29b-41d4-a716-446655440000"

	var sawWrite bool
	var sawRetrieve bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ping":
			writeJSON(t, w, map[string]string{"status": "ok"})
		case "/v1/version":
			writeVersion(t, w)
		case "/v1/features":
			writeFeatures(t, w)
		case "/v1/migrations":
			writeMigrations(t, w)
		case "/v1/namespaces/_doctor/write":
			sawWrite = true
			body := decodeMap(t, r)
			is.Equal(body["dedup"], true)
			is.Equal(body["source_id"], "contextdb-doctor")
			writeJSON(t, w, map[string]any{"node_id": nodeID, "admitted": true})
		case "/v1/namespaces/_doctor/retrieve":
			sawRetrieve = true
			body := decodeMap(t, r)
			is.Equal(body["top_k"], float64(5))
			writeJSON(t, w, map[string]any{
				"results": []map[string]any{
					{"id": nodeID, "score": 1.0, "similarity_score": 1.0},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report, err := doctor.Run(context.Background(), doctor.Options{BaseURL: srv.URL, SampleWrite: true})
	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.SampleNodeID, nodeID)
	is.Equal(len(report.Checks), 5)
	is.True(sawWrite)
	is.True(sawRetrieve)
}

func TestRun_SampleWriteFailsWhenRetrieveMissesNode(t *testing.T) {
	is := is.New(t)
	const nodeID = "550e8400-e29b-41d4-a716-446655440000"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ping":
			writeJSON(t, w, map[string]string{"status": "ok"})
		case "/v1/version":
			writeVersion(t, w)
		case "/v1/features":
			writeFeatures(t, w)
		case "/v1/migrations":
			writeMigrations(t, w)
		case "/v1/namespaces/_doctor/write":
			writeJSON(t, w, map[string]any{"node_id": nodeID, "admitted": true})
		case "/v1/namespaces/_doctor/retrieve":
			writeJSON(t, w, map[string]any{"results": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report, err := doctor.Run(context.Background(), doctor.Options{BaseURL: srv.URL, SampleWrite: true})
	is.NoErr(err)
	is.True(!report.OK)
	is.Equal(report.SampleNodeID, nodeID)
	is.Equal(report.Checks[len(report.Checks)-1].Name, "sample_write")
	is.True(!report.Checks[len(report.Checks)-1].OK)
}

func TestRun_ReportsFailedChecks(t *testing.T) {
	is := is.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ping":
			writeJSON(t, w, map[string]string{"status": "ok"})
		case "/v1/version":
			writeJSON(t, w, map[string]any{"version": "0.4.0", "api_version": "v1"})
		case "/v1/features":
			writeJSON(t, w, map[string]any{"version": "0.4.0", "features": []map[string]string{{"name": "rest-api"}}})
		case "/v1/migrations":
			writeJSON(t, w, map[string]any{"version": "0.4.0", "latest_migration": 0, "migrations": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report, err := doctor.Run(context.Background(), doctor.Options{BaseURL: srv.URL})
	is.NoErr(err)
	is.True(!report.OK)

	failures := 0
	for _, check := range report.Checks {
		if !check.OK {
			failures++
		}
	}
	is.Equal(failures, 2)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func writeVersion(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, map[string]any{
		"version":          "0.4.1",
		"api_version":      "v1",
		"latest_migration": 2,
		"features": []map[string]string{
			{"name": "feature-introspection", "status": "stable", "since": "v0.4.0"},
		},
		"migrations": []map[string]any{
			{"version": 1, "name": "initial"},
			{"version": 2, "name": "node_fingerprints"},
		},
	})
}

func writeFeatures(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, map[string]any{
		"version": "0.4.1",
		"features": []map[string]string{
			{"name": "feature-introspection", "status": "stable", "since": "v0.4.0"},
		},
	})
}

func writeMigrations(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, map[string]any{
		"version":          "0.4.1",
		"latest_migration": 2,
		"migrations": []map[string]any{
			{"version": 1, "name": "initial"},
			{"version": 2, "name": "node_fingerprints"},
		},
	})
}

func decodeMap(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
