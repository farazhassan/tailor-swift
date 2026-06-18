package embed

import (
	"fmt"
	"os"

	"github.com/farazhassan/gantry/components/embeddings"
	"github.com/farazhassan/gantry/components/embeddings/voyage"
)

const (
	defaultVoyageModel = "voyage-3"
	voyageModelEnv     = "VOYAGE_MODEL"
	voyageKeyEnv       = "VOYAGE_API_KEY"
)

// NewVoyageClient builds a Voyage embeddings client from the environment. The
// model comes from VOYAGE_MODEL (default "voyage-3"); the key from
// VOYAGE_API_KEY. It returns an error — rather than panicking — when the key is
// absent, so the CLI can report it cleanly.
func NewVoyageClient() (embeddings.Embeddings, error) {
	if os.Getenv(voyageKeyEnv) == "" {
		return nil, fmt.Errorf("embed: %s is not set", voyageKeyEnv)
	}
	model := os.Getenv(voyageModelEnv)
	if model == "" {
		model = defaultVoyageModel
	}
	return voyage.New(model), nil
}
