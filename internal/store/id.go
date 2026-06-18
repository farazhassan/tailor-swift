package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DeriveID returns a stable id for an achievement derived from its text.
// Text is normalized (lowercased, whitespace collapsed) so cosmetic edits
// do not change the id, but content changes do.
func DeriveID(text string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	sum := sha256.Sum256([]byte(norm))
	return "ach_" + hex.EncodeToString(sum[:])[:12]
}
