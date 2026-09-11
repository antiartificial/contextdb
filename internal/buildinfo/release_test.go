package buildinfo

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestReleaseMetadataMatchesImplementation(t *testing.T) {
	for _, path := range []string{"../../package.json", "../../sdk/typescript/package.json"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Version != Version {
			t.Fatalf("%s version %s, build %s", path, manifest.Version, Version)
		}
	}
	b, err := os.ReadFile("../../sdk/python/pyproject.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "version = \""+Version+"\"") {
		t.Fatal("Python version differs from build")
	}
	b, err = os.ReadFile("../../docs/release-health.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		var minor int
		if _, err := fmt.Sscanf(line, "| v0.%d.0 |", &minor); err == nil && minor >= 109 && minor <= 122 {
			t.Fatal("unverified historical proposals must not be certified by release health")
		}
	}
}
