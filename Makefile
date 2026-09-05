.PHONY: help tools run dev watch build test test-race cover vuln vet lint fmt tidy demo clean

.DEFAULT_GOAL := help

GOLANGCI_VERSION := v2.13.2
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

help: ## Shows this help
	@printf "Available targets:\n\n"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\nDev flows: make tools (one-time), make dev (one-shot), make watch (hot-reload).\n"

AIR_VERSION := v1.67.4
GOVULNCHECK_VERSION := v1.7.0

tools: ## Installs dev tools: air, govulncheck and golangci-lint
	go install github.com/air-verse/air@$(AIR_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_VERSION)

run: ## Runs the server without dev defaults (needs JWT_SECRET, like prod)
	go run ./cmd/api

dev: ## One-shot dev server with defaults (no .env needed)
	@./scripts/dev.sh

watch: ## Hot-reload with air (requires: make tools)
	@air -c .air.toml

build: ## Builds the binary at bin/pinolrent-api with the build version baked in
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/pinolrent-api ./cmd/api

test: ## Runs the test suite (no cache of results)
	go test -count=1 -timeout 180s ./...

test-race: ## Runs the test suite with the race detector (requires CGO/gcc)
	CGO_ENABLED=1 go test -count=1 -race -timeout 300s ./...

vuln: ## Runs govulncheck
	govulncheck ./...

COVER_MIN ?= 70
cover: ## Runs the test suite and prints a one-line coverage summary (COVER_MIN=N to enforce a gate)
	go test -count=1 -timeout 180s -coverprofile=cover.out -covermode=atomic ./...
	@go tool cover -func=cover.out | tail -n1
	@pct=$$(go tool cover -func=cover.out | tail -n1 | grep -o '[0-9.]*%' | tr -d '%'); rm -f cover.out; \
	if [ "$(COVER_MIN)" != "0" ]; then echo "coverage $${pct}% (min $(COVER_MIN)%)"; awk -v p="$${pct}" -v m="$(COVER_MIN)" 'BEGIN{exit (p+0 < m+0)}'; fi

vet: ## Runs go vet
	go vet ./...

lint: ## Runs golangci-lint (requires: make tools)
	@golangci-lint run ./...

fmt: ## Formats the code with gofmt
	@gofmt -l -w .

tidy: ## Tidies go.mod/go.sum
	go mod tidy

demo: ## Self-contained end-to-end smoke (ephemeral server on :8132)
	@./scripts/demo.sh

clean: ## Removes bin/, tmp/, air.log and the dev database
	rm -rf bin tmp air.log dev.db dev.db-shm dev.db-wal