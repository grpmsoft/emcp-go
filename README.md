# eMCP Go SDK

Official Go SDK for the Enhanced Model Context Protocol (eMCP) - an enterprise-grade extension of MCP with checkpoint support, risk assessment, and multi-transport capabilities.

## Features

- ✅ **100% MCP Compatible** - Works with any MCP client/server
- ✅ **Multiple Transports** - stdio (MCP), gRPC, WebSocket, HTTP
- ✅ **Checkpoint System** - Time-travel debugging and state recovery
- ✅ **Risk Assessment** - Automatic risk evaluation for operations
- ✅ **Type Safety** - Full type definitions and compile-time checks
- ✅ **Streaming Support** - Real-time notifications and updates

## Installation

**Requirements:**
- Go 1.25+ (required for json/v2 support)
- `GOEXPERIMENT=jsonv2` environment variable

```bash
go get github.com/goco-ai/emcp-go
```

Or add to your go.work for local development:
```bash
use ./emcp-go
```

**Note:** eMCP-go uses Go's `encoding/json/v2` package which requires Go 1.25+ and the `GOEXPERIMENT=jsonv2` flag. JSON v2 will become stable in Go 1.26 (February 2026).

## Quick Start

### Server Example (Working Implementation)

```go
package main

import (
    "context"
    "encoding/json/v2"
    "log"
    "github.com/goco-ai/emcp-go/emcp"
    "github.com/goco-ai/emcp-go/server"
)

func main() {
    // Create server with middleware
    srv := server.New(
        "my-server",
        "1.0.0",
        server.WithMiddleware(server.LoggingMiddleware(log.Printf)),
    )

    // Register a tool
    tool := emcp.ToolDefinition{
        Name:        "echo",
        Description: "Echoes back the input message",
        InputSchema: &emcp.JSONSchema{
            Type: "object",
            Properties: map[string]*emcp.SchemaProperty{
                "message": {
                    Type:        "string",
                    Description: "Message to echo",
                },
            },
            Required: []string{"message"},
        },
        RiskLevel: emcp.RiskLow,
    }

    srv.AddTool(tool, func(ctx context.Context, params []byte) ([]byte, error) {
        var input struct {
            Message string `json:"message"`
        }
        if err := json.Unmarshal(params, &input); err != nil {
            return nil, err
        }

        result := map[string]string{"echo": input.Message}
        return json.Marshal(result)
    })

    // Start stdio transport (MCP compatible)
    transport := server.NewStdioTransport(srv)
    log.Fatal(transport.Serve())
}
```

### Client Example (Working Implementation)

```go
package main

import (
    "context"
    "log"
    "github.com/goco-ai/emcp-go/client"
)

func main() {
    // Create stdio transport (launches server subprocess)
    transport, err := client.NewStdioTransport("./server.exe")
    if err != nil {
        log.Fatal(err)
    }
    defer transport.Close()

    // Create client
    c := client.New(transport)
    defer c.Close()

    ctx := context.Background()

    // Initialize connection
    if err := c.Initialize(ctx); err != nil {
        log.Fatal(err)
    }

    // List available tools
    tools, _ := c.ListTools(ctx)
    log.Printf("Available tools: %d", len(tools))

    // Call a tool
    result, err := c.CallTool(ctx, "echo", map[string]any{
        "message": "Hello from client!",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Result: %s", result)
}
```

See `examples/client-demo/` for full CLI example with JSON output support.

## Core Concepts

### Checkpoints

Checkpoints provide time-travel debugging capabilities:

```go
// Create checkpoint
checkpoint, err := client.CreateCheckpoint(ctx, 
    "Before database migration",
    []string{"database", "migration"},
)

// Do risky operation...

// If something goes wrong, restore
if err != nil {
    client.RestoreCheckpoint(ctx, checkpoint.ID)
}
```

### Risk Assessment

All operations have risk levels:

```go
tool := emcp.Tool{
    Name:               "delete_data",
    RiskLevel:         emcp.RiskHigh,
    RequiresCheckpoint: true,  // Auto checkpoint
}
```

Risk Levels:
- `RiskLow` - Safe operations
- `RiskMedium` - May modify state
- `RiskHigh` - Significant changes
- `RiskCritical` - Irreversible operations

### Transports

#### stdio (MCP Default)
```go
transport := emcp.NewStdioTransport()
```

#### gRPC (High Performance)
```go
transport := emcp.NewGRPCTransport("localhost:8095")
```

#### WebSocket (Real-time)
```go
transport := emcp.NewWebSocketTransport("ws://localhost:8097")
```

## API Reference

### Client Methods

| Method | Description |
|--------|-------------|
| `Initialize(ctx)` | Connect to server |
| `ListTools(ctx)` | Get available tools |
| `CallTool(ctx, name, args)` | Execute tool |
| `CallToolWithCheckpoint(ctx, name, args, reason)` | Execute with checkpoint |
| `CreateCheckpoint(ctx, desc, tags)` | Manual checkpoint |
| `RestoreCheckpoint(ctx, id)` | Restore state |
| `ListResources(ctx)` | Get resources |
| `ReadResource(ctx, uri)` | Read resource |

### Server Methods

| Method | Description |
|--------|-------------|
| `RegisterTool(tool, handler)` | Add tool |
| `RegisterResource(uri, provider)` | Add resource |
| `RegisterPrompt(prompt)` | Add prompt |
| `Start(ctx)` | Start server |
| `Stop()` | Stop server |

## Advanced Features

### Notifications

Subscribe to real-time updates:

```go
client.OnNotification("tools/list_changed", func(method string, params json.RawMessage) {
    log.Println("Tools updated!")
})
```

### Context Helpers

Add metadata to context:

```go
ctx = emcp.WithSession(ctx, "session-123")
ctx = emcp.WithRiskLevel(ctx, emcp.RiskHigh)
ctx = emcp.WithCheckpoint(ctx, "checkpoint-456")
```

### Custom Transports

Implement the `Transport` interface:

```go
type Transport interface {
    Send(ctx context.Context, method string, params interface{}) (json.RawMessage, error)
    SendNotification(ctx context.Context, method string, params interface{}) error
    Close() error
    OnNotification(handler NotificationHandler)
}
```

## Compatibility

| eMCP Version | MCP Version | Go Version | Notes |
|--------------|-------------|------------|-------|
| 0.1.x | 1.0 | 1.25+ | Requires `GOEXPERIMENT=jsonv2` |

## Examples

See the [examples](examples/) directory for complete examples:
- [Client Example](examples/client/main.go)
- [Server Example](examples/server/main.go)

## Contributing

Contributions welcome! Please read our [Contributing Guide](CONTRIBUTING.md).

## License

MIT License - see [LICENSE](LICENSE) file.

## Links

- [eMCP Specification](https://github.com/emcp-protocol/specification)
- [MCP Documentation](https://modelcontextprotocol.io)
- [GODA Project](https://github.com/goco-ai/goda)