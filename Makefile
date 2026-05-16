.PHONY: build test check run lint tidy bench setup-hooks

VERSION ?= $(shell cat VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/jazz1x/rallish/internal/buildinfo.version=$(VERSION) \
           -X github.com/jazz1x/rallish/internal/buildinfo.commit=$(COMMIT) \
           -X github.com/jazz1x/rallish/internal/buildinfo.date=$(DATE)

# Prefer the repo-pinned linter (matches lefthook + CI) and fall back to
# whatever golangci-lint is on $PATH.
GOLANGCI_LINT := $(shell \
	if [ -x "$(PWD)/.toolchain/bin/golangci-lint" ]; then \
		echo "$(PWD)/.toolchain/bin/golangci-lint"; \
	else \
		command -v golangci-lint 2>/dev/null || echo ""; \
	fi)

build:
	go build -ldflags "$(LDFLAGS)" -o dist/rallish ./cmd/rallish

test:
	go test ./...

check:
	@if [ -z "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found. Install it or run via .toolchain (make setup-hooks first)."; \
		exit 1; \
	fi
	go vet ./...
	@PATH="$(PWD)/.toolchain/go/bin:$(PWD)/.toolchain/bin:$$PATH" "$(GOLANGCI_LINT)" run --timeout=5m
	go test ./... -race

run: build
	./dist/rallish

lint:
	@if [ -z "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found. Install it or run via .toolchain (make setup-hooks first)."; \
		exit 1; \
	fi
	@PATH="$(PWD)/.toolchain/go/bin:$(PWD)/.toolchain/bin:$$PATH" "$(GOLANGCI_LINT)" run --timeout=5m

tidy:
	go mod tidy
	go mod verify

bench:
	go test -bench=. -benchmem ./...

setup-hooks:
	@if [ -f "$(PWD)/.toolchain/bin/lefthook" ]; then \
		"$(PWD)/.toolchain/bin/lefthook" install; \
	else \
		which lefthook > /dev/null 2>&1 || go install github.com/evilmartians/lefthook@latest; \
		lefthook install; \
	fi
	@bash scripts/patch-lefthook.sh

update-version:
	@bash scripts/update-version.sh
