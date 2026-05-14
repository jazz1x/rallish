.PHONY: build test check run lint tidy bench

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/jazz1x/hocketty/internal/buildinfo.version=$(VERSION) \
           -X github.com/jazz1x/hocketty/internal/buildinfo.commit=$(COMMIT) \
           -X github.com/jazz1x/hocketty/internal/buildinfo.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o dist/hocketty ./cmd/hocketty

test:
	go test ./...

check:
	go vet ./... && golangci-lint run && go test ./... -race

run: build
	./dist/hocketty

lint:
	golangci-lint run

tidy:
	go mod tidy
	go mod verify

bench:
	go test -bench=. -benchmem ./...
