BINARY := soko
BUILD_DIR := .

.PHONY: build test lint clean

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/soko

test:
	go test -race ./...

lint:
	golangci-lint run

clean:
	rm -f $(BUILD_DIR)/$(BINARY)
