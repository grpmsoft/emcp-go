// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package emcp

import "time"

// Transport selects the transport protocol for connecting to an MCP server.
type Transport string

const (
	// TransportHTTP connects via Streamable HTTP (the standard MCP transport).
	TransportHTTP Transport = "http"

	// TransportGRPC connects via gRPC bidirectional stream (emcp-go extension).
	TransportGRPC Transport = "grpc"

	// TransportStdio connects via stdin/stdout to a child process.
	TransportStdio Transport = "stdio"

	// TransportAuto tries gRPC first, then falls back to HTTP.
	TransportAuto Transport = "auto"
)

// Config holds all parameters needed to connect a Client to an MCP server.
type Config struct {
	// Endpoint is the server address.
	//   - For HTTP: full URL like "http://localhost:8094/mcp"
	//   - For gRPC: host:port like "localhost:50051"
	Endpoint string

	// PIDFile is the path to a daemon PID file (e.g. ".gode/gode.pid").
	// When set and Endpoint is empty, the client reads port and token from
	// the PID file. PIDFile takes precedence over BearerToken.
	PIDFile string

	// Transport selects the protocol. Defaults to TransportAuto.
	Transport Transport

	// Command is the executable path for stdio transport.
	Command string

	// Args are the command-line arguments for stdio transport.
	Args []string

	// BearerToken is the bearer token for HTTP authentication.
	// When PIDFile is set, the token is read from the PID file automatically.
	BearerToken string

	// Timeout is the per-call timeout. Zero means no timeout.
	Timeout time.Duration
}
