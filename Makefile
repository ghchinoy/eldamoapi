# Eldamo MCP Server Makefile

# Go configuration
BINARY_NAME=eldamoapi
ADMIN_NAME=eldamo-admin
BIN_DIR=bin
BINARY_PATH=$(BIN_DIR)/$(BINARY_NAME)
ADMIN_PATH=$(BIN_DIR)/$(ADMIN_NAME)

.PHONY: all build run test clean generate help

all: generate test build

## build: Build the statically linked Go binaries to ./bin
build:
	@echo "Building server binary to $(BINARY_PATH)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY_PATH) .
	@echo "Building admin binary to $(ADMIN_PATH)..."
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(ADMIN_PATH) ./cmd/eldamo-admin/
	@echo "✓ Build complete."

## run: Run the server locally in unprotected mode
run: build
	@echo "Starting server locally..."
	./$(BINARY_PATH)

## run-dev: Run the server locally with authentication bypass
run-dev: build
	@echo "Starting server locally with AUTH_BYPASS=true..."
	AUTH_BYPASS=true ./$(BINARY_PATH)

## test: Run all unit and integration tests
test:
	@echo "Running all tests..."
	go test -v ./...

## admin: Run the admin tool interactively
admin:
	@go run ./cmd/eldamo-admin/ list

## token: Generate a 1-hour secure Access Token JWT for testing (override with UID=<uid>)
## NOTE: Run as: set -a; source .env; set +a; make token
## so that JWT_SIGNING_KEY is exported to child processes before minting.
UID ?= dev-user
token:
	@go run ./cmd/eldamo-admin/ token $(UID)

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
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\8997//' | sed -e 's/##/ /'
