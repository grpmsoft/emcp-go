# eMCP gRPC Demo

Demonstration of eMCP with high-performance gRPC transport.

## Overview

This example shows how to use eMCP SDK with gRPC transport for binary, efficient communication between client and server.

## Features

- **Protocol Buffers v3** - Binary serialization (faster than JSON)
- **HTTP/2** - Multiplexing, flow control, header compression
- **Type-safe API** - Compile-time checking with .proto definitions
- **Bidirectional streaming** - Real-time communication support
- **Built-in TLS** - Secure communication ready

## Running the Demo

### 1. Start the Server

```bash
# Build
go build -o grpc-demo .

# Run server
./grpc-demo server
```

Output:
```
🚀 Starting eMCP gRPC Server...
==================================================
✅ Server listening on :50051
📦 Registered tools: echo, uppercase, add

Press Ctrl+C to stop...
```

### 2. Run the Client (in another terminal)

```bash
./grpc-demo client
```

Output:
```
🔌 eMCP gRPC Client Demo
==================================================

📡 Connecting to localhost:50051...
✅ Connected!

🤝 Initializing...
✅ Connected to: demo-server v1.0.0 (MCP 2024-11-05)

📋 Available tools:
  • echo - Echoes back the input message (risk: low)
  • uppercase - Converts text to uppercase (risk: low)
  • add - Adds two numbers (risk: low)

🔧 Calling tools:

1. Echo tool:
  ✅ Result: {"echo":"Hello from gRPC!"}

2. Uppercase tool:
  ✅ Result: {"result":"GRPC IS AWESOME"}

3. Add tool:
  ✅ Result: {"result":100}

✅ Demo completed successfully!
```

## Code Structure

### Server (lines 42-84)

```go
// Create server with tools
srv := server.New("demo-server", "1.0.0")
srv.AddTool(echoTool, echoHandler)
srv.AddTool(uppercaseTool, uppercaseHandler)
srv.AddTool(addTool, addHandler)

// Create gRPC transport
transport := server.NewGRPCTransport(srv, ":50051",
    server.WithVerbose(true))

// Start server (blocking)
transport.Serve()
```

### Client (lines 86-166)

```go
// Connect to gRPC server
transport, err := client.NewGRPCTransport("localhost:50051")
defer transport.Close()

// Initialize connection
info, err := transport.Initialize(ctx, clientInfo)

// List available tools
tools, err := transport.ListTools(ctx)

// Call tools
result, err := transport.CallTool(ctx, "echo", args)
```

## Registered Tools

### 1. echo
Echoes back the input message

**Input:**
```json
{"message": "Hello!"}
```

**Output:**
```json
{"echo": "Hello!"}
```

### 2. uppercase
Converts text to uppercase

**Input:**
```json
{"text": "hello world"}
```

**Output:**
```json
{"result": "HELLO WORLD"}
```

### 3. add
Adds two numbers

**Input:**
```json
{"a": 42, "b": 58}
```

**Output:**
```json
{"result": 100}
```

## Transport Comparison

| Feature | stdio | HTTP | gRPC |
|---------|-------|------|------|
| Format | JSON | JSON | Binary |
| Speed | Fast | Medium | Very Fast |
| Overhead | Low | Medium | Lowest |
| Clients | Single | Multiple | Multiple |
| Streaming | No | Limited | Bidirectional |
| Type Safety | Runtime | Runtime | Compile-time |

## Production Usage

### GOCO↔GODA Integration

GODA MCP server can be started with gRPC:
```bash
goda mcp serve-grpc --grpc-port 50051 --verbose
```

GOCO connects via ClientManager:
```go
manager := mcp.NewClientManager()
err := manager.ConnectGRPC("goda", "localhost:50051")
```

### TLS Configuration

For production, enable TLS:

**Server:**
```go
creds, _ := credentials.NewServerTLSFromFile("cert.pem", "key.pem")
transport := server.NewGRPCTransport(srv, ":50051",
    server.WithTLS(creds))
```

**Client:**
```go
creds := credentials.NewClientTLSFromFile("ca.pem", "")
transport, _ := client.NewGRPCTransport("localhost:50051",
    client.WithGRPCTLS(creds))
```

## Protocol Buffers

The gRPC transport uses Protocol Buffers v3 defined in:
```
proto/emcp/v1/emcp.proto
```

Generated code:
```
proto/emcp/v1/emcp.pb.go        # Message definitions
proto/emcp/v1/emcp_grpc.pb.go   # Service definitions
```

## Troubleshooting

### Connection Refused
```
Error: connection refused
```
**Solution:** Make sure server is running on the correct port.

### Import Errors
```
Error: missing go.sum entry for google.golang.org/grpc
```
**Solution:** Run `go mod tidy`

### GOEXPERIMENT Error
```
Error: package encoding/json/v2 is not in GOROOT
```
**Solution:** Set environment variable:
```bash
export GOEXPERIMENT=jsonv2
```

## Performance Metrics

Based on internal benchmarks (GOCO→GODA):

| Operation | stdio | HTTP | gRPC |
|-----------|-------|------|------|
| Connection | 100ms | 50ms | 30ms |
| Tool Call | 10ms | 20ms | 5ms |
| Throughput | 100 ops/s | 200 ops/s | 500 ops/s |

## Next Steps

- Explore [basic example](../basic/) for stdio transport
- Read [client-demo](../client-demo/) for CLI integration
- Check [eMCP Specification](../../specification/) for protocol details

## References

- [gRPC Documentation](https://grpc.io/docs/languages/go/)
- [Protocol Buffers Guide](https://protobuf.dev/getting-started/gotutorial/)
- [eMCP Protocol Spec](../../specification/protocol.md)
