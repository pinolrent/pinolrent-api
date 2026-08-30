.PHONY: help tools run dev watch build test vet lint fmt tidy demo clean

.DEFAULT_GOAL := help

GOLANGCI_VERSION := v2.13.2

help: ## Shows this help
	@printf "Available targets:\n\n"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\nDev flows: make tools (one-time), make dev (one-shot), make watch (hot-reload).\n"

tools: ## Installs dev tools: air (go install) and golangci-lint (official binary)
	go install github.com/air-verse/air@latest
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_VERSION)

run: ## Runs the server with the current environment (no reload)
	@./scripts/dev.sh

dev: run ## Alias of run: one-shot dev server

watch: ## Hot-reload with air (requires: make tools)
	@air -c .air.toml

build: ## Builds the binary at bin/pinolrent-api
	@mkdir -p bin
	go build -o bin/pinolrent-api ./cmd/api

test: ## Runs the test suite (no cache of results)
	go test -count=1 -timeout 120s ./...

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

clean: ## Removes bin/ and the dev database
	rm -rf bin dev.db dev.db-shm dev.db-wal