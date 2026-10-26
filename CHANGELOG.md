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
- `Tool` (with `InputSchema`/`OutputSchema` overrides) and `ToolAnnotations` (title, read-only,
  destructive, idempotent, open-world hints).
- `InferSchema[T]` to start from the inferred schema and add enums, ranges and defaults.
- `ToolInfo.OutputSchema` and `ToolInfo.Annotations` on the client side.
- Typed tools accept `"arguments": null` (sent for a call without arguments) also when their
  schema has defaults: go-sdk v1.8.0 panics on it (fixed upstream in 0f18bc76, #1343, unreleased),
  so a receiving middleware treats null as absent until emcp-go moves to the next go-sdk release.
- A panic during a tool call, in the handler or in the SDK's argument handling, becomes a tool
  error naming the tool instead of ending the server process; one bad call no longer cuts off
  every client of a shared daemon. The stack goes to `ServerConfig.Logger`.
- `ServerConfig.Logger` (`*slog.Logger`, default `slog.Default()`) for emcp's own logs.
- Over HTTP, a client that disconnects cancels the running tool handler's ctx (go-sdk's
  `PropagateRequestCancellation`), for clients on protocol 2026-07-28 or later, such as the emcp
  client. Older clients cannot cancel a running call, so long tools still need their own bound.
  `ServerConfig.DisableRequestCancellation` turns it off.

### Changed

- **Breaking (0.x minor):** `ToolResult.StructuredContent` is `json.RawMessage` instead of
  `map[string]any`, so any JSON value fits (SEP-2106 allows arrays and primitives; the map dropped
  them) and callers decode it into their own type. Migration: a client replaces
  `res.StructuredContent["k"]` with `json.Unmarshal(res.StructuredContent, &out)`; a server sets
  `StructuredContent: json.RawMessage(...)` or uses a typed tool, whose `Out` fills it.
- `github.com/google/jsonschema-go` is a direct dependency: `*jsonschema.Schema` appears in the
  public API (`Tool`, `InferSchema`), as in the official SDK.

### Security

- `golang.org/x/net` v0.57.0 → v0.61.0 (with `x/sync`, `x/sys`, `x/text`): fixes the HTTP/2
  advisories GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617, reachable
  through the gRPC transport and the HTTP client.

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
