.DEFAULT_GOAL := build

GO ?= go
GOFMT ?= gofmt
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/qbitctl
PACKAGES ?= ./...
CLI_PKG ?= ./cmd/qbitctl
APP_PKG ?= ./pkg/client/...
GOFILES := $(shell find cmd internal pkg -name '*.go' -type f | sort)
CGO_ENABLED ?= 0
VERSION ?= dev-$(shell git rev-parse --short=8 HEAD 2>/dev/null || echo unknown)
LDFLAGS ?= -X github.com/naterator/qbitctl/pkg/client.Version=$(VERSION) -s -w
COVERPKG ?= github.com/naterator/qbitctl/cmd/qbitctl,github.com/naterator/qbitctl/pkg/client,github.com/naterator/qbitctl/internal/configfile,github.com/naterator/qbitctl/internal/releaseattest,github.com/naterator/qbitctl/internal/searchresults,github.com/naterator/qbitctl/internal/tui
COVER_DIR ?= .cover
COVER_OUT ?= cover.out

# Override Go's usual cache with make GOCACHE=/tmp/qbitctl-gocache if needed.

.PHONY: help build run test test-app test-cli test-race vet fmt fmt-check \
        tidy check smoke cover check-coverage release-check ci clean

help: ## Show available targets
	@printf "Targets:\n"
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the qbitctl binary
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CLI_PKG)

run: ## Run qbitctl without building a binary first
	$(GO) run $(CLI_PKG)

test: ## Run the Go test suite
	$(GO) test $(PACKAGES)

test-app: ## Run tests for the pkg/client library
	$(GO) test $(APP_PKG)

test-cli: ## Run tests for the qbitctl command
	$(GO) test $(CLI_PKG)

test-race: ## Run the Go test suite with the race detector
	$(GO) test -race $(PACKAGES)

vet: ## Run go vet
	$(GO) vet $(PACKAGES)

fmt: ## Format Go source files
	$(GOFMT) -w $(GOFILES)

fmt-check: ## Check whether Go source files are formatted
	@test -z "$$($(GOFMT) -l $(GOFILES))" || (echo "Run 'make fmt' to format files"; $(GOFMT) -l $(GOFILES); exit 1)

tidy: ## Tidy Go module dependencies
	$(GO) mod tidy

check: test vet fmt-check ## Run test, vet, and format check

smoke: ## Build a smoke binary and run the native and PTY smoke tests
	$(GO) build -trimpath -o qbitctl-smoke $(CLI_PKG)
	python3 scripts/smoke_native.py ./qbitctl-smoke
	python3 scripts/smoke_pty.py ./qbitctl-smoke

cover: ## Merge unit and smoke coverage into cover.out
	rm -rf -- "$(COVER_DIR)" "$(COVER_OUT)" qbitctl-cover
	mkdir -p "$(COVER_DIR)/unit" "$(COVER_DIR)/int"
	$(GO) test -count=1 -cover -coverpkg=$(COVERPKG) ./... -args "-test.gocoverdir=$(CURDIR)/$(COVER_DIR)/unit"
	$(GO) build -trimpath -cover -coverpkg=$(COVERPKG) -o qbitctl-cover $(CLI_PKG)
	GOCOVERDIR="$(CURDIR)/$(COVER_DIR)/int" python3 scripts/smoke_native.py ./qbitctl-cover
	GOCOVERDIR="$(CURDIR)/$(COVER_DIR)/int" python3 scripts/smoke_pty.py ./qbitctl-cover
	$(GO) tool covdata textfmt "-i=$(COVER_DIR)/unit,$(COVER_DIR)/int" -o "$(COVER_OUT)"
	$(GO) tool cover "-func=$(COVER_OUT)" | tail -1

check-coverage: cover ## Check merged coverage against the ratcheted floor
	bash scripts/check_coverage.sh "$(COVER_OUT)" .coverage-floor

release-check: ## Validate the release helpers and target matrix
	sh scripts/test_release_helpers.sh
	sh scripts/test_release_targets.sh
	bash scripts/test_release_assets.sh

ci: fmt-check vet test-race smoke release-check ## Run the native CI job locally

clean: ## Remove built binaries
	rm -f $(BINARY) $(BINARY).exe
