.DEFAULT_GOAL := build

GO ?= go
GOFMT ?= gofmt
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/qbitctl
PACKAGES ?= ./...
CLI_PKG ?= ./cmd/qbitctl
APP_PKG ?= ./pkg/client/...
GOFILES := $(shell find cmd pkg -name '*.go' -type f | sort)
GOCACHE ?= /tmp/qbitctl-gocache
CGO_ENABLED ?= 0
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X github.com/naterator/qbitctl/pkg/client.Version=$(VERSION)

export GOCACHE

.PHONY: help build run test test-app test-cli vet fmt fmt-check tidy check clean

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

test-app: ## Run tests for the internal client package
	$(GO) test $(APP_PKG)

test-cli: ## Run tests for the merged Cobra CLI package
	$(GO) test $(CLI_PKG)

vet: ## Run go vet
	$(GO) vet $(PACKAGES)

fmt: ## Format Go source files
	$(GOFMT) -w $(GOFILES)

fmt-check: ## Check whether Go source files are formatted
	@test -z "$$($(GOFMT) -l $(GOFILES))" || (echo "Run 'make fmt' to format files"; $(GOFMT) -l $(GOFILES); exit 1)

tidy: ## Tidy Go module dependencies
	$(GO) mod tidy

check: test vet fmt-check ## Run test, vet, and format check

clean: ## Remove built binaries
	rm -f $(BINARY) $(BINARY).exe
