.PHONY: build test lint clean install help

BINARY     := relay
CMD        := ./cmd/relay
VERSION    := 1.0.0
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
LDFLAGS    := -X github.com/relaydev/relay/internal/daemon.BuildVersion=$(VERSION) \
              -X github.com/relaydev/relay/internal/daemon.BuildCommit=$(COMMIT) \
              -X github.com/relaydev/relay/internal/daemon.BuildDate=$(BUILD_DATE)

## build: Build the relay binary
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)
	@echo "Built: $(BINARY)"

## build-all: Build for multiple platforms
build-all:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/relay-darwin-arm64 $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/relay-darwin-amd64 $(CMD)
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/relay-linux-amd64  $(CMD)
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/relay-linux-arm64  $(CMD)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/relay-windows-amd64.exe $(CMD)
	@ls -lh dist/

## test: Run all tests
test:
	go test ./tests/... -v -timeout 60s

## test-short: Run tests without verbose output
test-short:
	go test ./tests/... -timeout 60s

## lint: Run linter (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null || (echo "Install: brew install golangci-lint" && exit 1)
	golangci-lint run ./...

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/
	go clean -testcache

## install: Install relay to GOBIN or ~/go/bin
install:
	go install -ldflags "$(LDFLAGS)" $(CMD)
	@echo "Installed to $(shell go env GOPATH)/bin/relay"

## run: Build and start daemon in foreground (for development)
run: build
	./relay daemon-run --base-dir /tmp/relay-dev

## up: Build and start daemon in background
up: build
	./relay init 2>/dev/null || true
	./relay up

## down: Stop daemon
down:
	./relay down

## doctor: Run diagnostics
doctor: build
	./relay doctor

## example: Run the Python example (daemon must be running)
example:
	python3 examples/python/example.py

## example-loop: Run the agent loop example
example-loop:
	python3 examples/python/agent_loop.py

## help: Show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
