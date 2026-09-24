# eMCP — Enterprise MCP Extensions for Go

Enterprise extensions for the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). Adds daemon integration, risk assessment, metrics, and enterprise middleware to standard MCP servers and clients.

## Architecture

eMCP is **not** a standalone MCP SDK. It builds on the official SDK maintained by Google:

```
Your MCP Server / Client
    │
    ├── github.com/goco-ai/emcp-go/daemontx     ← Daemon transport
    ├── github.com/goco-ai/emcp-go/middleware    ← Enterprise middleware
    ├── github.com/goco-ai/emcp-go/toolmeta      ← Tool annotations
    │
    └── github.com/modelcontextprotocol/go-sdk   ← Official MCP SDK (core)
```

## Packages

### `daemontx` — Daemon Transport

Connect to MCP servers running as local daemons with automatic port/token discovery.

```go
import "github.com/goco-ai/emcp-go/daemontx"

transport := &daemontx.DaemonTransport{
    PIDFilePath: "/path/to/workspace/.gode/gode.pid",
}

client := mcp.NewClient(&mcp.Implementation{Name: "my-client", Version: "1.0"}, nil)
session, err := client.Connect(ctx, transport, nil)

// Use session — tools/list, tools/call, etc.
result, err := session.CallTool(ctx, &mcp.CallToolParams{
    Name:      "gode__search_symbols",
    Arguments: map[string]any{"query": "SessionManager"},
})
```

Features:
- **PID file discovery** — reads port + bearer token from daemon's PID file
- **Bearer auth** — injects `Authorization: Bearer <token>` via `http.RoundTripper`
- **SSE disabled by default** — compatible with plain JSON-RPC daemon servers
- **Test-friendly** — inject custom `*http.Client` for `httptest.NewServer` testing

### `middleware` — Enterprise Middleware

Production-grade middleware implementing the official SDK's `mcp.Middleware` interface.

```go
import "github.com/goco-ai/emcp-go/middleware"

server := mcp.NewServer(impl, nil)

server.AddReceivingMiddleware(
    middleware.Recovery(logger),                           // catch panics
    middleware.Metrics(recorder),                          // per-method timing
    middleware.RiskGate(risks, middleware.MaxRisk(toolmeta.RiskMedium)),  // block high-risk tools
    middleware.ToolTimeout(timeouts, 30*time.Second),      // per-tool deadlines
)
```

| Middleware | Purpose |
|---|---|
| **Recovery** | Catches panics, logs stack trace, returns JSON-RPC error |
| **Metrics** | `MetricsRecorder` interface with `InMemoryRecorder` implementation |
| **RiskGate** | Blocks tool calls above configured risk level |
| **ToolTimeout** | Per-tool `context.WithTimeout` from configurable map |

### `toolmeta` — Tool Annotations

Enterprise metadata for MCP tools, stored in `Tool.Meta` with `emcp:` namespace.

```go
import "github.com/goco-ai/emcp-go/toolmeta"

tool := &mcp.Tool{Name: "delete_database", Description: "Drop all tables"}
toolmeta.SetRiskLevel(tool, toolmeta.RiskCritical)
toolmeta.SetRequiresCheckpoint(tool, true)

// Later, in middleware or client code:
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
go test ./toolmeta/ ./middleware/ ./daemontx/ -count=1
```

38 tests across all packages. Integration tests use `mcp.NewInMemoryTransports()` and `httptest.NewServer` — no external services required.

## Legacy Packages

The `client/`, `server/`, and `emcp/` packages are from eMCP v0.1 and are **deprecated**. They will be removed in a future version. Use the official SDK directly for core MCP functionality.

## Related Projects

- [GODE](https://github.com/goco-ai/gode) — Headless Go IDE with 65 MCP tools (primary consumer)
- [GOCO](https://github.com/goco-ai/goco) — MAP agent framework (uses daemontx for GODE connection)
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) — Core MCP implementation by Google

## License

MIT
