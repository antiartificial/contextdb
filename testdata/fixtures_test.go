package testdata

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicRetryFatiguePresetSchemaMatchesFixture(t *testing.T) {
	path := filepath.Join("..", "docs", "public", "schemas", "retry-fatigue-presets.schema.json")
	publicSchema, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(publicSchema, RetryFatiguePresetSchemaJSON) {
		t.Fatalf("%s does not match embedded retry fatigue preset fixture", path)
	}
}

func TestPublicSchemaIndexCatalogsRetryFatiguePresetSchema(t *testing.T) {
	indexPath := filepath.Join("..", "docs", "public", "schemas", "index.json")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		SchemaVersion int `json:"schema_version"`
		Schemas       []struct {
			ID           string `json:"id"`
			Href         string `json:"href"`
			JSONSchemaID string `json:"json_schema_id"`
			Feature      string `json:"feature"`
			Owner        string `json:"owner"`
			IntroducedIn string `json:"introduced_in"`
			CatalogedIn  string `json:"cataloged_in"`
			Status       string `json:"status"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 {
		t.Fatalf("schema catalog version = %d, want 1", catalog.SchemaVersion)
	}
	if len(catalog.Schemas) != 1 {
		t.Fatalf("schema catalog entries = %d, want 1", len(catalog.Schemas))
	}
	entry := catalog.Schemas[0]
	if entry.ID != "retry-fatigue-presets" {
		t.Fatalf("schema id = %q, want retry-fatigue-presets", entry.ID)
	}
	if entry.Href != "/contextdb/schemas/retry-fatigue-presets.schema.json" {
		t.Fatalf("schema href = %q", entry.Href)
	}
	if entry.JSONSchemaID != "https://antiartificial.github.io/contextdb/schemas/retry-fatigue-presets.schema.json" {
		t.Fatalf("schema JSON id = %q", entry.JSONSchemaID)
	}
	if entry.Feature != "review-handoff-retry-fatigue-preset-schema-publication" {
		t.Fatalf("schema feature = %q", entry.Feature)
	}
	if entry.Owner != "review-handoff" {
		t.Fatalf("schema owner = %q", entry.Owner)
	}
	if entry.IntroducedIn != "v0.97.0" || entry.CatalogedIn != "v0.100.0" || entry.Status != "stable" {
		t.Fatalf("schema release metadata = introduced %q cataloged %q status %q", entry.IntroducedIn, entry.CatalogedIn, entry.Status)
	}
	publicSchemaPath := filepath.Join("..", "docs", "public", "schemas", "retry-fatigue-presets.schema.json")
	if _, err := os.Stat(publicSchemaPath); err != nil {
		t.Fatalf("cataloged schema artifact missing: %v", err)
	}
}

func TestPublicFixtureIndexCatalogsKVRefreshReceiptFixtures(t *testing.T) {
	indexPath := filepath.Join("..", "docs", "public", "fixtures", "index.json")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		SchemaVersion int `json:"schema_version"`
		Fixtures      []struct {
			ID                     string `json:"id"`
			Href                   string `json:"href"`
			Feature                string `json:"feature"`
			Owner                  string `json:"owner"`
			Kind                   string `json:"kind"`
			ExpectedVerifierStatus string `json:"expected_verifier_status"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 {
		t.Fatalf("fixture catalog version = %d, want 1", catalog.SchemaVersion)
	}
	if len(catalog.Fixtures) != 3 {
		t.Fatalf("fixture catalog entries = %d, want 3", len(catalog.Fixtures))
	}
	statusByID := map[string]string{}
	for _, entry := range catalog.Fixtures {
		if entry.Feature != "derived-kv-refresh-receipt-verification" {
			t.Fatalf("fixture %s feature = %q", entry.ID, entry.Feature)
		}
		if entry.Owner != "operations" {
			t.Fatalf("fixture %s owner = %q", entry.ID, entry.Owner)
		}
		if entry.Kind == "" {
			t.Fatalf("fixture %s kind is empty", entry.ID)
		}
		path := filepath.Join("..", "docs", "public", filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(entry.Href, "/contextdb/"), "/")))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cataloged fixture %s missing: %v", entry.ID, err)
		}
		statusByID[entry.ID] = entry.ExpectedVerifierStatus
	}
	if statusByID["kv-refresh-valid-receipt"] != "passed" {
		t.Fatalf("valid receipt status = %q", statusByID["kv-refresh-valid-receipt"])
	}
	if statusByID["kv-refresh-valid-value"] != "input" {
		t.Fatalf("valid value status = %q", statusByID["kv-refresh-valid-value"])
	}
	if statusByID["kv-refresh-mismatched-value"] != "failed" {
		t.Fatalf("mismatched value status = %q", statusByID["kv-refresh-mismatched-value"])
	}
}
