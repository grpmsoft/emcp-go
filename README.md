# eMCP — Enterprise MCP Extensions for Go

Enterprise extensions for the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). Adds gRPC bidirectional stream transport, risk assessment, metrics, and production middleware to any MCP server.

## Architecture

eMCP is a thin extension layer on top of the official SDK -- not a standalone implementation:

```
Your MCP Server
    |
    |-- github.com/goco-ai/emcp-go/grpc           <-- gRPC bidi stream transport
    |-- github.com/goco-ai/emcp-go/middleware      <-- Enterprise middleware
    |-- github.com/goco-ai/emcp-go/toolmeta        <-- Tool annotations
    |
    +-- github.com/modelcontextprotocol/go-sdk     <-- Official MCP SDK (core)
```

## Packages

### `grpc` -- gRPC Bidirectional Stream Transport

The core package. Bridges MCP over gRPC bidirectional streaming, enabling persistent connections with full-duplex communication, connection multiplexing, TLS, and gRPC middleware.

**Server side:**

```go
import (
    emcpgrpc "github.com/goco-ai/emcp-go/grpc"
    emcpv1 "github.com/goco-ai/emcp-go/grpc/proto/emcpv1"
    "google.golang.org/grpc"
)

mcpServer := mcp.NewServer(impl, nil)
// ... add tools, prompts, resources ...

grpcServer := grpc.NewServer()
handler := emcpgrpc.NewGRPCHandler(func() *mcp.Server { return mcpServer })
emcpv1.RegisterMCPTransportServer(grpcServer, handler)
grpcServer.Serve(listener)
```

**Client side:**

```go
transport := &emcpgrpc.GRPCTransport{
    Target:      "localhost:50051",
    DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
}
client := mcp.NewClient(impl, nil)
session, err := client.Connect(ctx, transport, nil)

// Use session exactly like any other MCP transport:
tools, _ := session.ListTools(ctx, nil)
result, _ := session.CallTool(ctx, &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "Alice"}})
```

**Protocol:** JSON-RPC messages are serialized via `jsonrpc.EncodeMessage` and transported as opaque byte payloads inside protobuf `MCPMessage` frames. The gRPC layer never inspects the JSON-RPC content.

```protobuf
service MCPTransport {
  rpc Stream(stream MCPMessage) returns (stream MCPMessage);
}

message MCPMessage {
  bytes payload = 1;  // JSON-RPC message bytes
}
```

### `middleware` -- Enterprise Middleware

Production middleware implementing the official SDK's `mcp.Middleware` interface.

```go
import "github.com/goco-ai/emcp-go/middleware"

server := mcp.NewServer(impl, nil)

server.AddReceivingMiddleware(
    middleware.Recovery(logger),                                          // catch panics
    middleware.Metrics(recorder),                                         // per-method timing
    middleware.RiskGate(policy, risks),                                   // block high-risk tools
    middleware.Timeout(timeouts, 30*time.Second),                         // per-tool deadlines
)
```

| Middleware | Purpose |
|---|---|
| **Recovery** | Catches panics, logs stack trace via `slog.Logger`, returns JSON-RPC internal error |
| **Metrics** | `MetricsRecorder` interface with thread-safe `InMemoryRecorder` (atomic counters) |
| **RiskGate** | `RiskPolicy` interface -- blocks tool calls above configured risk level |
| **Timeout** | Per-tool `context.WithTimeout` from `map[string]time.Duration` |

### `toolmeta` -- Tool Annotations

Enterprise metadata for MCP tools, stored in `Tool.Meta` with `emcp:` namespace prefix.

```go
import "github.com/goco-ai/emcp-go/toolmeta"

tool := &mcp.Tool{Name: "delete_database", Description: "Drop all tables"}
toolmeta.SetRiskLevel(tool, toolmeta.RiskCritical)
toolmeta.SetRequiresCheckpoint(tool, true)

level := toolmeta.GetRiskLevel(tool)           // RiskCritical
needsCP := toolmeta.RequiresCheckpoint(tool)   // true
```

Risk levels: `RiskLow`, `RiskMedium`, `RiskHigh`, `RiskCritical`.

## Installation

```bash
go get github.com/goco-ai/emcp-go@latest
```

Requires Go 1.27+.

## Testing

```bash
go test ./... -count=1
```

37 tests. gRPC tests use `bufconn` for in-memory transport (no TCP, no ports). Middleware tests use `mcp.NewInMemoryTransports()`.

## Related Projects

- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) -- Core MCP implementation by Google
- [GODE](https://github.com/goco-ai/gode) -- Headless Go IDE with 65 MCP tools
- [GOCO](https://github.com/goco-ai/goco) -- MAP agent framework

## License

MIT
