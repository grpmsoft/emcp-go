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

```bash
go get github.com/goco-ai/goda/sdk/go/emcp
```

## Quick Start

### Client Example

```go
package main

import (
    "context"
    "log"
    "github.com/goco-ai/goda/sdk/go/emcp"
)

func main() {
    // Create client with stdio transport (MCP compatible)
    transport := emcp.NewStdioTransport()
    client, err := emcp.NewClient(&emcp.Options{
        Transport:         transport,
        EnableCheckpoints: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()
    
    // Initialize connection
    ctx := context.Background()
    if err := client.Initialize(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Call tool with automatic checkpoint
    result, err := client.CallToolWithCheckpoint(ctx, 
        "risky_operation",
        map[string]interface{}{"param": "value"},
        "Before risky operation",
    )
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Result: %v", result)
}
```

### Server Example

```go
package main

import (
    "context"
    "log"
    "github.com/goco-ai/goda/sdk/go/emcp"
)

func main() {
    // Create server
    transport := emcp.NewStdioTransport()
    server, err := emcp.NewServer(&emcp.Options{
        Transport:         transport,
        EnableCheckpoints: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Register tool
    tool := emcp.Tool{
        Name:        "echo",
        Description: "Echo message",
        RiskLevel:   emcp.RiskLow,
    }
    
    server.RegisterTool(tool, func(ctx context.Context, args map[string]interface{}) (*emcp.CallToolResult, error) {
        return &emcp.CallToolResult{
            Content: []emcp.Content{{
                Type: "text",
                Text: args["message"].(string),
            }},
        }, nil
    })
    
    // Start server
    ctx := context.Background()
    if err := server.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

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

| eMCP Version | MCP Version | Go Version |
|--------------|-------------|------------|
| 0.1.x | 1.0 | 1.19+ |

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