package client

import (
	"strings"
	"testing"
)

func TestExplicitBackendModesFailClosed(t *testing.T) {
	for _, opts := range []Options{{Mode: ModeStandard}, {Mode: ModeStandard, DSN: "  "}, {Mode: ModeScaled}, {Mode: ModeScaled, DSN: "postgres://unused"}} {
		db, err := Open(opts)
		if err == nil {
			db.Close()
			t.Fatalf("mode %q unexpectedly opened", opts.Mode)
		}
	}
}

func TestEffectiveEmbeddedBackends(t *testing.T) {
	for _, dir := range []string{"", t.TempDir()} {
		db, err := Open(Options{DataDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		info := db.Backends()
		if info.Mode != ModeEmbedded || info.Persistent != (dir != "") {
			t.Fatalf("unexpected backends: %+v", info)
		}
		if strings.Contains(info.Graph, dir) && dir != "" {
			t.Fatal("backend metadata exposes data path")
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
