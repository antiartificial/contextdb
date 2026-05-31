package testdata

import (
	"bytes"
	"os"
	"path/filepath"
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
