package core_test

import (
	"testing"

	"github.com/antiartificial/contextdb/internal/core"
)

func TestContentFingerprint_NormalizesCaseWhitespaceAndPunctuation(t *testing.T) {
	a := core.ContentFingerprint("  Go, uses   GC! ")
	b := core.ContentFingerprint("go uses gc")
	if a == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if a != b {
		t.Fatalf("fingerprints differ: %q != %q", a, b)
	}
}

func TestContentFingerprint_EmptyAfterNormalization(t *testing.T) {
	if got := core.ContentFingerprint(" \t !!! "); got != "" {
		t.Fatalf("expected empty fingerprint, got %q", got)
	}
}
