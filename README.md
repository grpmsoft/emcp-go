# emcp-go

[![CI](https://github.com/grpmsoft/emcp-go/actions/workflows/ci.yml/badge.svg)](https://github.com/grpmsoft/emcp-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/grpmsoft/emcp-go.svg)](https://pkg.go.dev/github.com/grpmsoft/emcp-go)
[![codecov](https://codecov.io/gh/grpmsoft/emcp-go/branch/main/graph/badge.svg)](https://codecov.io/gh/grpmsoft/emcp-go)
[![Go Version](https://img.shields.io/github/go-mod/go-version/grpmsoft/emcp-go)](https://github.com/grpmsoft/emcp-go/blob/main/go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**Enterprise MCP Extensions for Go -- unified client+server library built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).**

Adds gRPC bidirectional stream transport, PID file discovery, and bearer auth to any MCP server or client.

```
Your CLI / Agent
    |
    +-- emcp.NewClient(Config)
    |     |-- HTTP (Streamable HTTP)
    |     |-- gRPC (bidirectional stream)
    |     +-- stdio (child process)
    |
    +-- emcp.NewServer(ServerConfig)
          |-- srv.HTTPHandler()   --> http.Handler
          +-- srv.GRPCHandler()   --> grpc.Server
          |
          +-- github.com/modelcontextprotocol/go-sdk  (official MCP SDK)
```

## Why

The official MCP Go SDK provides the protocol implementation. emcp-go adds what you need in production:

- **One client for all transports** -- `NewClient(Config{Endpoint, Transport})` connects via HTTP, gRPC, or stdio. No transport-specific boilerplate.
- **gRPC transport** -- full-duplex bidirectional streaming, connection multiplexing, TLS, and gRPC middleware. JSON-RPC messages are transported as opaque bytes inside protobuf frames.
- **Daemon discovery** -- `Config{PIDFile: ".gode/gode.pid"}` reads port and bearer token from a daemon PID file automatically.
- **Clean public API** -- official SDK types are never exposed. Your code depends on `emcp.ToolResult`, not `mcp.CallToolResult`.

## Installation

```bash
go get github.com/grpmsoft/emcp-go@latest
```

Requires Go 1.27+.

## Quick Start

### Client

```go
client, err := emcp.NewClient(emcp.Config{
    Endpoint:  "http://localhost:8094/mcp",
    Transport: emcp.TransportHTTP,
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// List available tools:
tools, err := client.ListTools(ctx)

// Call a tool:
result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hello"})

// Health check:
err = client.Ping(ctx)
```

### Server

```go
srv := emcp.NewServer(emcp.ServerConfig{
    Name:    "my-server",
    Version: "1.0.0",
})

srv.AddTool("echo", "echoes the input", map[string]any{
    "type": "object",
    "properties": map[string]any{
        "msg": map[string]any{"type": "string"},
    },
}, func(ctx context.Context, name string, args map[string]any) (*emcp.ToolResult, error) {
    msg, _ := args["msg"].(string)
    return emcp.TextResult("echo: " + msg), nil
})

mux := http.NewServeMux()
mux.Handle("/mcp", srv.HTTPHandler())
http.ListenAndServe(":8080", mux)
```

## Three Modes

### Client Mode

Connect to any MCP server with a single call:

```go
// HTTP
client, _ := emcp.NewClient(emcp.Config{
    Endpoint:  "http://localhost:8080/mcp",
    Transport: emcp.TransportHTTP,
})

// gRPC
client, _ := emcp.NewClient(emcp.Config{
    Endpoint:  "localhost:50051",
    Transport: emcp.TransportGRPC,
})

// Daemon discovery (reads port + token from PID file)
client, _ := emcp.NewClient(emcp.Config{
    PIDFile: ".gode/gode.pid",
})
```

### Server Mode

Serve MCP tools over HTTP and/or gRPC from the same server instance:

```go
srv := emcp.NewServer(emcp.ServerConfig{Name: "tools", Version: "1.0.0"})
srv.AddTool("greet", "say hello", schema, handler)

// HTTP
mux.Handle("/mcp", srv.HTTPHandler())

// gRPC (same server instance)
emcpv1.RegisterMCPTransportServer(grpcServer, srv.GRPCHandler())
```

### gRPC Transport

The `grpc/` package implements MCP over gRPC bidirectional streaming:

```protobuf
service MCPTransport {
  rpc Stream(stream MCPMessage) returns (stream MCPMessage);
}

message MCPMessage {
  bytes payload = 1;  // JSON-RPC message bytes
}
```

JSON-RPC messages are serialized via the official SDK and transported as opaque byte payloads. The gRPC layer never inspects the JSON-RPC content.

The `grpc/typed/` package provides an alternative with native protobuf messages for each MCP method, using the Google canonical proto for MCP-over-gRPC.

## API Reference

### Client

| Method | Signature | Description |
|--------|-----------|-------------|
| `NewClient` | `(Config) (*Client, error)` | Create and connect |
| `ListTools` | `(ctx) ([]*ToolInfo, error)` | List available tools |
| `CallTool` | `(ctx, name, args) (*ToolResult, error)` | Invoke a tool |
| `Ping` | `(ctx) error` | Verify connection |
| `Close` | `() error` | Release resources |

### Server

| Method | Signature | Description |
|--------|-----------|-------------|
| `NewServer` | `(ServerConfig) *Server` | Create server |
| `AddTool` | `(name, desc, schema, handler)` | Register a tool |
| `HTTPHandler` | `() http.Handler` | Streamable HTTP handler |
| `GRPCHandler` | `() *grpc.GRPCHandler` | gRPC stream handler |
| `Close` | `() error` | Release resources |

### Convenience

| Function | Description |
|----------|-------------|
| `TextResult(text)` | Single text content item |
| `ErrorResult(msg)` | Text content with `IsError=true` |
| `ReadPIDFile(path)` | Parse daemon PID file |

### Config Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Endpoint` | `string` | -- | Server address (HTTP URL or gRPC host:port) |
| `PIDFile` | `string` | -- | Daemon PID file path for auto-discovery |
| `Transport` | `Transport` | `TransportAuto` | `TransportHTTP`, `TransportGRPC`, `TransportStdio`, `TransportAuto` |
| `Command` | `string` | -- | Executable path for stdio transport |
| `Args` | `[]string` | -- | Arguments for stdio transport |
| `BearerToken` | `string` | -- | HTTP auth; auto-read from PID file |
| `Timeout` | `time.Duration` | `0` (none) | Per-call timeout |

## Package Structure

```
github.com/grpmsoft/emcp-go
├── emcp.go          -- Client (public API)
├── server.go        -- Server (public API)
├── config.go        -- Config, Transport enum
├── types.go         -- ToolInfo, ToolResult, ContentItem
├── discovery.go     -- DaemonInfo, ReadPIDFile
├── grpc/            -- gRPC bidirectional stream transport
│   ├── transport.go -- GRPCTransport (mcp.Transport)
│   ├── handler.go   -- GRPCHandler (gRPC -> mcp.Server)
│   ├── connection.go
│   ├── errors.go
│   ├── proto/emcpv1/ -- protobuf service definition
│   └── typed/       -- Typed gRPC (Google canonical proto)
└── internal/        -- Implementation (not importable)
    ├── mcpclient/   -- Client internals
    └── mcpserver/   -- Server internals
```

## FAQ

**Q: Do I need emcp-go to use MCP in Go?**

No. The [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) works standalone. emcp-go adds gRPC transport, unified client, and daemon discovery on top.

**Q: Does emcp-go support Streamable HTTP?**

Yes. The `HTTPHandler()` method uses `mcp.NewStreamableHTTPHandler` from the official SDK.

**Q: Can I use HTTP and gRPC from the same server?**

Yes. Create one `emcp.Server`, call both `HTTPHandler()` and `GRPCHandler()`.

**Q: What about SSE transport?**

Not yet. See [ROADMAP.md](ROADMAP.md).

## Related Projects

- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) -- Core MCP implementation
- [GODE](https://github.com/goco-ai/gode) -- Headless Go IDE with 65 MCP tools (primary consumer)
- [grpmsoft/daemon](https://github.com/grpmsoft/daemon) -- On-demand daemon lifecycle (PID file format)

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines and [CHANGELOG.md](CHANGELOG.md) for release history.

## License

Apache License 2.0. See [LICENSE](LICENSE).
