package core

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// ContentFingerprint returns a stable hash for semantically identical content
// that differs only in case, whitespace, or punctuation.
func ContentFingerprint(s string) string {
	normalized := normalizeFingerprintContent(s)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func normalizeFingerprintContent(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
