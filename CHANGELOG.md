# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-09-25

### Added

- Unified Client: HTTP, gRPC, stdio transports via `emcp.NewClient(Config)`
- Unified Server: `emcp.NewServer(ServerConfig)` with `HTTPHandler()` and `GRPCHandler()`
- gRPC bidirectional stream transport (`grpc/` package)
- Typed gRPC with Google canonical proto (`grpc/typed/` package)
- PID file discovery for daemon-managed servers
- Bearer token authentication for HTTP transport
- `TextResult()` and `ErrorResult()` convenience constructors
