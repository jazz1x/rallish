.PHONY: build test check run lint tidy bench setup-hooks

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/jazz1x/rallish/internal/buildinfo.version=$(VERSION) \
           -X github.com/jazz1x/rallish/internal/buildinfo.commit=$(COMMIT) \
           -X github.com/jazz1x/rallish/internal/buildinfo.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o dist/rallish ./cmd/rallish

test:
	go test ./...

check:
	go vet ./... && golangci-lint run && go test ./... -race

run: build
	./dist/rallish

lint:
	golangci-lint run

tidy:
	go mod tidy
	go mod verify

bench:
	go test -bench=. -benchmem ./...

setup-hooks:
	@which lefthook > /dev/null 2>&1 || go install github.com/evilmartians/lefthook@latest
	lefthook install
