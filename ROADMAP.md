# Roadmap

## v0.1.0 (current)

See [CHANGELOG.md](CHANGELOG.md) for full feature list.

Key capabilities: unified Client + Server on official MCP Go SDK v1.8.0, HTTP/gRPC stream/stdio transports, bearer auth (client + server), stateless by default (MCP 2026-07-28), PID file discovery, session liveness, retry-once.

gRPC typed transport (`grpc/typed/`) is **experimental**: Google canonical proto (`GoogleCloudPlatform/mcp-grpc-transport-proto@1d2216c`), standalone tool registry, 2 of 8 RPCs implemented (ListTools, CallTool).

## Future

- Bridge typed gRPC to shared `*mcp.Server` (no separate tool registry)
- Remaining 6 typed RPCs (resources, prompts, complete)
- Reconnection strategy for long-lived clients
- Middleware hooks for client-side request/response interception
- Logging middleware with structured slog output
- Prometheus-compatible MetricsRecorder

## v1.0.0 — Stability

- Stable public API (no breaking changes after 1.0)
- `example_test.go` (pkg.go.dev runnable examples)
- OpenSSF Scorecard badge

## Non-Goals

- **Standalone MCP implementation**: emcp-go builds on the official SDK, not replaces it
- **Daemon management**: use [grpmsoft/daemon](https://github.com/grpmsoft/daemon) for process lifecycle
- **Protocol-level changes**: MCP protocol evolution is upstream's responsibility
