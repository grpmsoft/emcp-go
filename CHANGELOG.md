# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Typed tools: `emcp.AddTool[In, Out](s, Tool, TypedToolHandler[In, Out])` over the official
  SDK's `mcp.AddTool`. The input schema is inferred from `In` (descriptions from the `jsonschema`
  struct tag), arguments are validated against it with defaults applied before the handler runs,
  `Out` is returned as structured content with an output schema, and validation failures and
  handler errors reach the client as tool errors. Served by `HTTPHandler` and `GRPCHandler`
  behind the same authentication as untyped tools.
- `Tool` (with `InputSchema`/`OutputSchema` overrides) and `ToolAnnotations` (read-only,
  destructive, idempotent, open-world hints).
- `InferSchema[T]` to start from the inferred schema and add enums, ranges and defaults.
- `ToolInfo.OutputSchema` and `ToolInfo.Annotations` on the client side.

### Fixed

- A `tools/call` with `"arguments": null` to a tool whose schema has defaults no longer crashes
  the server. The SDK (v1.8.0) decoded null into a nil map and panicked writing the defaults into
  it, in the request goroutine, which ends the process. A receiving middleware now treats null
  arguments as absent. Workaround until the go-sdk release after v1.8.0 (upstream fix 0f18bc76,
  #1343).
- A panic during a tool call, in the handler or in the SDK's argument handling, becomes a tool
  error naming the tool instead of ending the server process; the stack goes to `slog.Default`.
  One bad call no longer cuts off every client of a shared daemon.

### Changed

- `github.com/google/jsonschema-go` is a direct dependency: `*jsonschema.Schema` appears in the
  public API (`Tool`, `InferSchema`), as in the official SDK.

## [0.1.1] - 2026-10-02

### Added

- Unified Client: HTTP, gRPC, stdio transports via `emcp.NewClient(Config)`
- Unified Server: `emcp.NewServer(ServerConfig)` with `HTTPHandler()` and `GRPCHandler()`
- gRPC bidirectional stream transport (`grpc/` package, implements `mcp.Transport`)
- Typed gRPC transport (`grpc/typed/` package, experimental) — Google canonical proto (`GoogleCloudPlatform/mcp-grpc-transport-proto@1d2216c`), standalone tool registry, 2 of 8 RPCs implemented (ListTools, CallTool)
- PID file discovery for daemon-managed servers
- Bearer token authentication: client-side injection (HTTP + gRPC) and server-side verification (`ServerConfig.TokenValidator`)
- `StaticToken()` helper with `subtle.ConstantTimeCompare`
- `TextResult()` and `ErrorResult()` convenience constructors
- Content types: text, image, audio, resource, resource_link, StructuredContent
- Stateless HTTP server by default (MCP 2026-07-28 spec)
- Session liveness via `done` channel from `session.Wait()`
- Retry-once on transport error (server restart recovery)
- `buf.yaml` + `buf.gen.yaml` for reproducible proto generation

### Architecture

- Implementation in `internal/mcpclient/` and `internal/mcpserver/` — official MCP Go SDK types never exposed
- Public API: `emcp.Client`, `emcp.Server`, `emcp.ToolInfo`, `emcp.ToolResult`, `emcp.ContentItem`
- Official MCP Go SDK (github.com/modelcontextprotocol/go-sdk) as sole core dependency
