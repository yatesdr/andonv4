# Andon v4 Makefile

BINARY_NAME=andon
VERSION?=0.1.0
BUILD_DIR=build

LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: all clean linux windows macos build run test

all: clean linux windows macos

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/andon

run:
	go run ./cmd/andon

test:
	go test -v ./...

linux: linux-amd64 linux-arm64

linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/andon

linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/andon

windows: windows-amd64

windows-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/andon

macos: macos-amd64 macos-arm64

macos-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/andon

macos-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/andon

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)

deps:
	go mod tidy
	go mod download

fmt:
	go fmt ./...

help:
	@echo "Andon v4 Build Targets:"
	@echo "  make build     - Build for current platform"
	@echo "  make run       - Run the application"
	@echo "  make all       - Build for all platforms"
	@echo "  make linux     - Build for Linux (amd64, arm64)"
	@echo "  make windows   - Build for Windows (amd64)"
	@echo "  make macos     - Build for macOS (amd64, arm64)"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make test      - Run tests"
	@echo "  make deps      - Update dependencies"
	@echo "  make fmt       - Format code"
