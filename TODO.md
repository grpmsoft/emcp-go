# eMCP TODO

## Done (v2.0)

- [x] `toolmeta` — RiskLevel + RequiresCheckpoint via Tool.Meta (6 tests)
- [x] `middleware` — Recovery, Metrics, RiskGate, Timeout (22 tests)
- [x] Official MCP Go SDK as sole dependency (v1.8.0)
- [x] Legacy code removed (client/, server/, emcp/, proto/)

## Next

- [ ] Logging middleware with structured slog output
- [ ] Prometheus-compatible MetricsRecorder implementation
- [ ] gRPC transport (when SEP-1352 merges) — Google canonical proto

## ADR

See `goco-ai/docs/dev/adr/ADR-029-EMCP-MIGRATION-TO-OFFICIAL-SDK.md`
