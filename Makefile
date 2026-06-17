# Eldamo MCP Server Makefile

# Go configuration
BINARY_NAME=eldamoapi
BIN_DIR=bin
BINARY_PATH=$(BIN_DIR)/$(BINARY_NAME)

.PHONY: all build run test clean generate help

all: generate test build

## build: Build the statically linked Go binary to ./bin
build:
	@echo "Building binary to $(BINARY_PATH)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY_PATH) .
	@echo "✓ Build complete: $(BINARY_PATH)"

## run: Run the server locally in unprotected mode
run: build
	@echo "Starting server locally..."
	./$(BINARY_PATH)

## test: Run all unit and integration tests
test:
	@echo "Running all tests..."
	go test -v ./...

## token: Generate a 1-hour secure Access Token JWT for testing
token:
	@go run scripts/gen-token/main.go

## generate: Run go generate to rebuild the embedded dataset (requires local raw XML files)
generate:
	@echo "Running code generation..."
	go generate ./...

## clean: Clean compiled binaries and local test artifacts
clean:
	@echo "Cleaning up..."
	rm -rf $(BIN_DIR)
	@echo "✓ Cleanup complete."

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##/ /'
