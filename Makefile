BINARY  := soko
MODULE  := github.com/CelikE/soko
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build install test test-v lint fmt tidy check clean

## build: Compile the binary
build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/soko

## install: Install the binary to $GOPATH/bin
install:
	go install -ldflags '$(LDFLAGS)' ./cmd/soko

## test: Run all tests with race detection
test:
	go test -race ./...

## test-v: Run all tests with verbose output
test-v:
	go test -race -v ./...

## lint: Run golangci-lint
lint:
	golangci-lint run

## fmt: Format code with goimports
fmt:
	goimports -w -local $(MODULE) .

## tidy: Tidy and verify go.mod
tidy:
	go mod tidy
	go mod verify

## check: Run fmt, lint, and tests (CI-ready)
check: fmt lint test

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
