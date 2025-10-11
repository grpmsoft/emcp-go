# eMCP Go SDK Makefile
.PHONY: help proto proto-check proto-lint proto-format test build clean install-tools

# Default target
help:
	@echo "eMCP Go SDK - Available targets:"
	@echo "  make proto         - Generate Go code from proto files"
	@echo "  make proto-check   - Check proto files for errors"
	@echo "  make proto-lint    - Lint proto files"
	@echo "  make proto-format  - Format proto files"
	@echo "  make test          - Run all tests"
	@echo "  make build         - Build all packages"
	@echo "  make clean         - Clean generated files"
	@echo "  make install-tools - Install required tools (buf, protoc-gen-go, etc.)"

# Install required tools
install-tools:
	@echo "Installing buf..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@echo "Installing protoc-gen-go..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "Installing protoc-gen-go-grpc..."
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "All tools installed successfully!"

# Generate Go code from proto files
proto:
	@echo "Generating Go code from proto files..."
	@protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--proto_path=proto \
		proto/emcp/v1/emcp.proto
	@mv emcp/v1/*.pb.go proto/emcp/v1/ 2>/dev/null || true
	@rmdir -p emcp/v1 2>/dev/null || true
	@echo "Proto generation complete!"

# Check proto files for breaking changes and errors
proto-check:
	@echo "Checking proto files..."
	@buf lint
	@buf breaking --against '.git#branch=main'
	@echo "Proto check complete!"

# Lint proto files
proto-lint:
	@echo "Linting proto files..."
	@buf lint
	@echo "Lint complete!"

# Format proto files
proto-format:
	@echo "Formatting proto files..."
	@buf format -w
	@echo "Format complete!"

# Run tests
test:
	@echo "Running tests..."
	@GOEXPERIMENT=jsonv2 go test -v -cover ./...
	@echo "Tests complete!"

# Build all packages
build:
	@echo "Building packages..."
	@GOEXPERIMENT=jsonv2 go build ./...
	@echo "Build complete!"

# Clean generated files
clean:
	@echo "Cleaning generated files..."
	@find proto -name "*.pb.go" -delete
	@find proto -name "*_grpc.pb.go" -delete
	@echo "Clean complete!"

# Development workflow
dev: proto-format proto build test
	@echo "Development workflow complete!"
