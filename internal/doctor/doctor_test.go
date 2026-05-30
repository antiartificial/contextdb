package doctor_test

import (
	"context"
	"encoding/json"
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
