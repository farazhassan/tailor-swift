package store

import "testing"

func TestDeriveID_StableAndPrefixed(t *testing.T) {
	a := DeriveID("Cut billing settlement latency 40%")
	b := DeriveID("Cut billing settlement latency 40%")
	if a != b {
		t.Fatalf("ids not stable: %q != %q", a, b)
	}
	if a[:4] != "ach_" {
		t.Fatalf("id %q missing ach_ prefix", a)
	}
	if len(a) != 16 { // "ach_" + 12 hex chars
		t.Fatalf("id %q length = %d, want 16", a, len(a))
	}
}

func TestDeriveID_NormalizesWhitespaceAndCase(t *testing.T) {
	if DeriveID("Hello   World") != DeriveID("hello world") {
		t.Fatal("expected normalized text to produce same id")
	}
}

func TestDeriveID_DiffersOnContent(t *testing.T) {
	if DeriveID("alpha") == DeriveID("beta") {
		t.Fatal("different text must produce different ids")
	}
}
