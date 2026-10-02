# emcp-go -- AI Assistant Guide

## What is emcp-go?

Enterprise MCP Extensions for Go -- a unified client+server library built on
top of the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
(v1.8.0). Published under [grpmsoft](https://github.com/grpmsoft), consumed
by GODE (headless Go IDE with 65 MCP tools, primary consumer).

emcp-go adds two capabilities the official SDK does not provide out of the box:

1. **Unified Client** -- connect to any MCP server via HTTP, gRPC, or stdio
   with a single `NewClient(Config)` call. Automatic PID file discovery for
   daemon-managed servers.

2. **gRPC Transport** -- bidirectional stream transport for MCP over gRPC.
   Full-duplex communication, connection multiplexing, TLS, and gRPC middleware.

Not a standalone MCP implementation. Not a daemon manager. Not middleware
(removed as too opinionated for a library).

## Architecture

```
emcp.go         -- Client: NewClient, ListTools, CallTool, Ping, Close
                   convertCallToolResult, convertContent
server.go       -- Server: NewServer, AddTool, HTTPHandler, GRPCHandler, Close
                   TextResult, ErrorResult, toInternalResult
config.go       -- Config (Endpoint, PIDFile, Transport, Command, Args,
                   BearerToken, Timeout), Transport enum
types.go        -- ToolInfo, ToolResult, ContentItem
discovery.go    -- DaemonInfo, ReadPIDFile, ErrPIDFileNotFound, ErrPIDFileEmpty
grpc/
  transport.go  -- GRPCTransport (mcp.Transport implementation)
  handler.go    -- GRPCHandler (bridges gRPC stream to mcp.Server)
  connection.go -- grpcConnection (concurrent-safe Write, implements mcp.Connection)
  errors.go     -- ErrStreamClosed, isStreamDone
  proto/emcpv1/ -- MCPTransport.Stream(MCPMessage) protobuf service
grpc/typed/
  client.go     -- TypedClient (MCP over typed gRPC RPCs)
  server.go     -- TypedServer (bridges typed RPCs to mcp.Server)
  convert.go    -- Protobuf <-> SDK type conversions
  proto/mcppb/  -- Google canonical proto for MCP-over-gRPC
internal/
  mcpclient/    -- Client implementation (session management, reconnect, bearer auth)
  mcpserver/    -- Server implementation (tool dispatch, argument extraction)
```

## Quick Start

### Client

```go
import "github.com/grpmsoft/emcp-go"

client, err := emcp.NewClient(emcp.Config{
    Endpoint:  "http://localhost:8094/mcp",
    Transport: emcp.TransportHTTP,
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

tools, _ := client.ListTools(ctx)
result, _ := client.CallTool(ctx, "echo", map[string]any{"msg": "hello"})
```

### Client with PID file discovery

```go
client, err := emcp.NewClient(emcp.Config{
    PIDFile: ".gode/gode.pid",
    // Port and bearer token are read from the PID file automatically.
})
```

### Server

```go
import "github.com/grpmsoft/emcp-go"

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

### gRPC Server

```go
import (
    emcpgrpc "github.com/grpmsoft/emcp-go/grpc"
    emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
    "google.golang.org/grpc"
)

grpcServer := grpc.NewServer()
emcpv1.RegisterMCPTransportServer(grpcServer, srv.GRPCHandler())
grpcServer.Serve(listener)
```

### gRPC Client

```go
import emcpgrpc "github.com/grpmsoft/emcp-go/grpc"

client, err := emcp.NewClient(emcp.Config{
    Endpoint:  "localhost:50051",
    Transport: emcp.TransportGRPC,
})
```

## Key Types

### Client

- **Config** -- connection parameters:
  - `Endpoint` (string) -- server address (HTTP URL or gRPC host:port)
  - `PIDFile` (string) -- daemon PID file path for auto-discovery
  - `Transport` (Transport) -- `TransportHTTP`, `TransportGRPC`, `TransportStdio`, `TransportAuto`
  - `Command` / `Args` (string / []string) -- for stdio transport
  - `BearerToken` (string) -- HTTP auth; auto-read from PID file if available
  - `Timeout` (time.Duration) -- per-call timeout (0 = no timeout)

- **Client** -- methods: `ListTools(ctx)`, `CallTool(ctx, name, args)`, `Ping(ctx)`, `Close()`

### Server

- **ServerConfig** -- server identity:
  - `Name` (string) -- server name sent in MCP initialize
  - `Version` (string) -- server version
  - `Instructions` (string) -- optional instructions

- **Server** -- methods: `AddTool(name, desc, schema, handler)`, `HTTPHandler()`, `GRPCHandler()`, `Close()`

- **ToolHandler** -- `func(ctx, name string, args map[string]any) (*ToolResult, error)`

### Result Types

- **ToolInfo** -- Name, Title, Description, InputSchema
- **ToolResult** -- Content []ContentItem, IsError bool
- **ContentItem** -- Type ("text"/"image"/"audio"), Text, MIMEType, Data

### Convenience Constructors

- **TextResult(text)** -- single-item text result
- **ErrorResult(msg)** -- text result with IsError=true

### PID File Discovery

- **DaemonInfo** -- PID, Port, Name, Token (from daemon PID file)
- **ReadPIDFile(path)** -- parses daemon PID file (JSON or plain-text fallback)
- **ErrPIDFileNotFound** / **ErrPIDFileEmpty** -- sentinel errors

## gRPC Transport

The `grpc/` package implements MCP over gRPC bidirectional streaming. JSON-RPC
messages are serialized and transported as opaque byte payloads inside protobuf
`MCPMessage` frames. The gRPC layer never inspects the JSON-RPC content.

```
service MCPTransport {
  rpc Stream(stream MCPMessage) returns (stream MCPMessage);
}

message MCPMessage {
  bytes payload = 1;
}
```

The `grpc/typed/` package (experimental) provides an alternative with native protobuf messages
for MCP methods (ListTools, CallTool implemented; 6 RPCs return Unimplemented) using the Google
canonical proto (`GoogleCloudPlatform/mcp-grpc-transport-proto@1d2216c`). Standalone tool registry.

## Ecosystem

| Repo | Relationship |
|------|-------------|
| [GODE](https://github.com/grpmsoft/gode) | Consumer -- headless IDE with 65 MCP tools |
| [grpmsoft/daemon](https://github.com/grpmsoft/daemon) | Complementary -- daemon lifecycle; PID file format |
| [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Foundation -- emcp-go builds on top |

## Development

```bash
go build ./...
go test ./...
go test -race ./...
go test -coverprofile=c.out ./...
golangci-lint run --timeout=5m
```
