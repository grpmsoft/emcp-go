# eMCP TODO

## Done (v2.0)

- [x] `toolmeta` — RiskLevel + RequiresCheckpoint via Tool.Meta (6 tests)
- [x] `middleware` — Recovery, Metrics, RiskGate, Timeout (22 tests)
- [x] Official MCP Go SDK as sole dependency (v1.8.0)
- [x] Legacy code removed (client/, server/, emcp/, proto/)

## Done (v3.0)

- [x] `grpc` — gRPC bidi stream transport (9 tests with bufconn)
  - GRPCTransport (client-side, implements mcp.Transport)
  - GRPCHandler (server-side, bridges gRPC stream to mcp.Server)
  - grpcConnection (implements mcp.Connection, concurrent-safe Write)
  - Proto: emcp.v1.MCPTransport.Stream(MCPMessage) returns (MCPMessage)
  - Tests: connect, ping, list tools, call tool, tool errors, concurrent calls,
    stream close, server shutdown, full integration with 3 tools

## Next

- [ ] Logging middleware with structured slog output
- [ ] Prometheus-compatible MetricsRecorder implementation

## ADR

See `goco-ai/docs/dev/adr/ADR-029-EMCP-MIGRATION-TO-OFFICIAL-SDK.md`
