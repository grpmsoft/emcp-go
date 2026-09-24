# eMCP — Enterprise MCP Extensions for Go

Enterprise extensions for the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). Adds risk assessment, metrics, and production middleware to any MCP server.

## Architecture

eMCP is a thin extension layer on top of the official SDK — not a standalone implementation:

```
Your MCP Server
    │
    ├── github.com/goco-ai/emcp-go/middleware    ← Enterprise middleware
    ├── github.com/goco-ai/emcp-go/toolmeta      ← Tool annotations
    │
    └── github.com/modelcontextprotocol/go-sdk   ← Official MCP SDK (core)
```

One direct dependency. Two packages. Pure MCP — no infrastructure coupling.

## Packages

### `middleware` — Enterprise Middleware

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
| **RiskGate** | `RiskPolicy` interface — blocks tool calls above configured risk level |
| **Timeout** | Per-tool `context.WithTimeout` from `map[string]time.Duration` |

### `toolmeta` — Tool Annotations

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

28 tests. Integration tests use `mcp.NewInMemoryTransports()` — no external services required.

## Related Projects

- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) — Core MCP implementation by Google
- [GODE](https://github.com/goco-ai/gode) — Headless Go IDE with 65 MCP tools
- [GOCO](https://github.com/goco-ai/goco) — MAP agent framework

## License

MIT
