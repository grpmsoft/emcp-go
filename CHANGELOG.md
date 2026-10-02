# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-10-02

### Added

- Unified Client: HTTP, gRPC, stdio transports via `emcp.NewClient(Config)`
- Unified Server: `emcp.NewServer(ServerConfig)` with `HTTPHandler()` and `GRPCHandler()`
- gRPC bidirectional stream transport (`grpc/` package, implements `mcp.Transport`)
- Typed gRPC transport (`grpc/typed/` package, experimental) — Google canonical proto (`GoogleCloudPlatform/mcp-grpc-transport-proto@1d2216c`), standalone tool registry, 2 of 8 RPCs implemented (ListTools, CallTool)
- PID file discovery for daemon-managed servers
- Bearer token authentication: client-side injection (HTTP + gRPC) and server-side verification (`ServerConfig.TokenValidator`)
- `StaticToken()` helper with `subtle.ConstantTimeCompare`
- `TextResult()` and `ErrorResult()` convenience constructors
- Content types: text, image, audio, embedded_resource, resource_link, StructuredContent
- Stateless HTTP server by default (MCP 2026-07-28 spec)
- Session liveness via `done` channel from `session.Wait()`
- Retry-once on transport error (server restart recovery)
- `buf.yaml` + `buf.gen.yaml` for reproducible proto generation

### Architecture

- Implementation in `internal/mcpclient/` and `internal/mcpserver/` — official MCP Go SDK types never exposed
- Public API: `emcp.Client`, `emcp.Server`, `emcp.ToolInfo`, `emcp.ToolResult`, `emcp.ContentItem`
- Official MCP Go SDK (github.com/modelcontextprotocol/go-sdk) as sole core dependency
