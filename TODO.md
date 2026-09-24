# eMCP TODO

## Done (v2.0)

- [x] `toolmeta` — RiskLevel + RequiresCheckpoint via Tool.Meta (6 tests)
- [x] `middleware` — Recovery, Metrics, RiskGate, ToolTimeout (22 tests)
- [x] `daemontx` — DaemonTransport with PID file discovery (10 tests)
- [x] Official MCP Go SDK as core dependency (v1.8.0)

## Next

- [ ] gRPC transport (`grpctx/`) — implement `mcp.Transport` using Google canonical proto
- [ ] Remove deprecated packages (`client/`, `server/`, `emcp/`) after GODE migration
- [ ] Logging middleware with structured slog output
- [ ] Prometheus-compatible metrics exporter
- [ ] Reconnection helper for DaemonTransport

## ADR

See `goco-ai/docs/dev/adr/ADR-029-EMCP-MIGRATION-TO-OFFICIAL-SDK.md`
