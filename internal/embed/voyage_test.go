package embed

import "testing"

func TestNewVoyageClientErrorsWithoutKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "")
	if _, err := NewVoyageClient(); err == nil {
		t.Error("NewVoyageClient: want error when VOYAGE_API_KEY unset, got nil")
	}
}

func TestNewVoyageClientBuildsWithKey(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "k")
	c, err := NewVoyageClient()
	if err != nil {
		t.Fatalf("NewVoyageClient: %v", err)
	}
	if c == nil {
		t.Error("NewVoyageClient returned nil client")
	}
}

func TestNewVoyageClientHonorsModelEnv(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "k")
	t.Setenv("VOYAGE_MODEL", "voyage-3-large")
	if _, err := NewVoyageClient(); err != nil {
		t.Fatalf("NewVoyageClient: %v", err)
	}
}
