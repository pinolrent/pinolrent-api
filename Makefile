.PHONY: help tools run dev watch build test test-race cover vet lint fmt tidy demo clean

.DEFAULT_GOAL := help

GOLANGCI_VERSION := v2.13.2
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

help: ## Shows this help
	@printf "Available targets:\n\n"
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\nDev flows: make tools (one-time), make dev (one-shot), make watch (hot-reload).\n"

AIR_VERSION := v1.67.4

tools: ## Installs dev tools: air (go install) and golangci-lint (official binary)
	go install github.com/air-verse/air@$(AIR_VERSION)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_VERSION)

run: ## Runs the server with the current environment (no reload)
	@./scripts/dev.sh

dev: run ## Alias of run: one-shot dev server

watch: ## Hot-reload with air (requires: make tools)
	@air -c .air.toml

build: ## Builds the binary at bin/pinolrent-api with the build version baked in
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/pinolrent-api ./cmd/api

test: ## Runs the test suite (no cache of results)
	go test -count=1 -timeout 120s ./...

test-race: ## Runs the test suite with the race detector (requires CGO/gcc)
	CGO_ENABLED=1 go test -count=1 -race -timeout 180s ./...

cover: ## Runs the test suite and prints a one-line coverage summary
	go test -count=1 -timeout 120s -coverprofile=cover.out -covermode=atomic ./...
	@go tool cover -func=cover.out | tail -n1
	@rm -f cover.out

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