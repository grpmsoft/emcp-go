# Roadmap

## Current State: v0.1.0

Enterprise MCP extensions for Go, built on the official MCP Go SDK.
Unified client+server with HTTP, gRPC, and stdio transports.

### What works

- Unified Client: `NewClient(Config)` with HTTP, gRPC, stdio transports
- Unified Server: `NewServer(ServerConfig)` with `HTTPHandler()` and `GRPCHandler()`
- gRPC bidirectional stream transport (`grpc/` package)
- Typed gRPC with Google canonical proto (`grpc/typed/` package)
- PID file discovery for daemon-managed servers
- Bearer token authentication for HTTP transport
- `TextResult()` and `ErrorResult()` convenience constructors
- 57 tests across 3 packages (root, grpc, grpc/typed)

## v0.2.0 -- Resilience

- [ ] Reconnection strategy for gRPC and HTTP transports
- [ ] Middleware hooks for client-side request/response interception
- [ ] Logging middleware with structured slog output
- [ ] Prometheus-compatible MetricsRecorder implementation

## v0.3.0 -- Transports

- [ ] SSE transport support
- [ ] Typed tool handlers with generic type parameters

## v1.0.0 -- Stability

- [ ] Stable public API (no breaking changes after 1.0)
- [ ] `example_test.go` (pkg.go.dev runnable examples)
- [ ] OpenSSF Scorecard badge

## Non-Goals

- **Standalone MCP implementation**: emcp-go builds on the official SDK, not replaces it
- **Daemon management**: use [grpmsoft/daemon](https://github.com/grpmsoft/daemon) for process lifecycle
- **Protocol-level changes**: MCP protocol evolution is upstream's responsibility
