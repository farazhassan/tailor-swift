# tailor-swift — developer helper targets.
# Run `make` (or `make help`) to list everything.

BINARY      := tailor
PKG         := ./cmd/tailor

# Override on the command line, e.g. `make validate CONTENT=resume.md`.
CONTENT     ?=
JD_URL      ?=
JD_FILE     ?=
PROVIDER    ?= openrouter
MODEL       ?=
MAX_ITER    ?= 3
OUT         ?= out
EMBED_CACHE ?= cache/embed.json
JD_CACHE    ?= cache/jd

# Caches are on by default so reruns on the same resume/JD are cheap. The
# optional --jd-file / --model flags are added only when their vars are set.
GEN_FLAGS := --content $(CONTENT) --jd-url $(JD_URL) --provider $(PROVIDER) \
             --out $(OUT) --max-iterations $(MAX_ITER) \
             --embed-cache $(EMBED_CACHE) --jd-cache $(JD_CACHE)
ifneq ($(strip $(JD_FILE)),)
GEN_FLAGS += --jd-file $(JD_FILE)
endif
ifneq ($(strip $(MODEL)),)
GEN_FLAGS += --model $(MODEL)
endif

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the tailor binary
	go build -o $(BINARY) $(PKG)

.PHONY: test
test: ## Run the unit suite (fakes/mock LLMs; no keys or network)
	go test ./...

.PHONY: fmt
fmt: ## Format all Go source
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: check
check: fmt vet test ## fmt + vet + test (the pre-push gate)

.PHONY: env
env: ## Create .env from env.sample (won't clobber an existing .env)
	@test -f .env && echo ".env already exists; leaving it untouched" || \
		{ cp env.sample .env && echo "created .env — fill in your keys, then: set -a; source .env; set +a"; }

.PHONY: validate
validate: build ## Parse a content store and print a summary (CONTENT=resume.md) — free, no API calls
	@test -n "$(CONTENT)" || { echo "set CONTENT=path/to/resume.md"; exit 2; }
	./$(BINARY) validate $(CONTENT)

.PHONY: generate
generate: build ## Full run (CONTENT=... JD_URL=... [JD_FILE=... PROVIDER=... MODEL=...]) — uses live keys
	@test -n "$(CONTENT)" || { echo "set CONTENT=path/to/resume.md"; exit 2; }
	@test -n "$(JD_URL)"  || { echo "set JD_URL=https://..."; exit 2; }
	./$(BINARY) generate $(GEN_FLAGS)

.PHONY: smoke
smoke: ## Cheapest end-to-end run: one iteration, caches on (same vars as generate)
	@$(MAKE) generate MAX_ITER=1

.PHONY: clean
clean: ## Remove the binary and generated out/ (keeps caches)
	rm -rf $(BINARY) $(OUT)

.PHONY: clean-cache
clean-cache: ## Remove cached embeddings and postings
	rm -rf cache
