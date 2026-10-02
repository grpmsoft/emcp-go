// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"context"
	"net/http"

	emcpgrpc "github.com/goco-ai/emcp-go/grpc"
	"github.com/goco-ai/emcp-go/internal/mcpserver"
)

// ToolHandler is the handler function for an MCP tool. It receives the tool
// name, decoded arguments as a map, and returns a ToolResult or error.
//
// Returning a non-nil error is treated as a protocol-level error (the call
// fails). To report an application-level error, return a ToolResult with
// IsError=true and a text content item describing the error.
type ToolHandler func(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

// ServerConfig holds parameters for creating a Server.
type ServerConfig struct {
	// Name identifies the server to connected clients (sent in MCP initialize).
	Name string
	// Version is the server version string (sent in MCP initialize).
	Version string
	// Instructions are optional instructions sent to clients via MCP initialize.
	Instructions string
}

// Server is a unified MCP server that supports HTTP and gRPC transports.
// It delegates all work to the internal mcpserver implementation, keeping
// the official SDK types out of the public API.
//
// Usage:
//
//	srv := emcp.NewServer(emcp.ServerConfig{Name: "my-server", Version: "1.0.0"})
//	srv.AddTool("echo", "echoes the input", nil, func(ctx context.Context, name string, args map[string]any) (*emcp.ToolResult, error) {
//	    msg, _ := args["msg"].(string)
//	    return emcp.TextResult("echo: " + msg), nil
//	})
//
//	mux := http.NewServeMux()
//	mux.Handle("/mcp", srv.HTTPHandler())
//	http.ListenAndServe(":8080", mux)
type Server struct {
	impl *mcpserver.Server
}

// NewServer creates a new MCP server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		impl: mcpserver.New(mcpserver.Config{
			Name:         cfg.Name,
			Version:      cfg.Version,
			Instructions: cfg.Instructions,
		}),
	}
}

// AddTool registers a tool with its handler. The inputSchema is a JSON Schema
// object (typically map[string]any) describing the tool's expected parameters.
// Pass nil for tools that accept no arguments.
func (s *Server) AddTool(name, description string, inputSchema map[string]any, handler ToolHandler) {
	s.impl.AddTool(name, description, inputSchema, func(ctx context.Context, n string, args map[string]any) (*mcpserver.ToolResult, error) {
		result, err := handler(ctx, n, args)
		if err != nil {
			return nil, err
		}
		return toInternalResult(result), nil
	})
}

// HTTPHandler returns an http.Handler for serving MCP over Streamable HTTP.
// Mount this on your HTTP mux: mux.Handle("/mcp", srv.HTTPHandler())
func (s *Server) HTTPHandler() http.Handler {
	return s.impl.HTTPHandler()
}

// GRPCHandler returns the gRPC handler for registering with a grpc.Server.
// Use: emcpv1.RegisterMCPTransportServer(grpcServer, srv.GRPCHandler())
func (s *Server) GRPCHandler() *emcpgrpc.GRPCHandler {
	return s.impl.GRPCHandler()
}

// Internal returns the underlying MCP server for advanced use cases.
// This is an escape hatch -- prefer the public API.
func (s *Server) Internal() any {
	return s.impl.MCPServer()
}

// Close shuts down the server and releases all resources.
func (s *Server) Close() error {
	// The SDK's mcp.Server does not have a Close method; tools and handlers
	// are garbage collected. This method exists for API symmetry with Client
	// and for future resource cleanup.
	return nil
}

// TextResult is a convenience constructor for a ToolResult with a single text
// content item. This is the most common result type.
func TextResult(text string) *ToolResult {
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: text}},
	}
}

// ErrorResult is a convenience constructor for a ToolResult that represents
// an application-level error. The error message is returned as text content
// with IsError=true.
func ErrorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: msg}},
	}
}

// toInternalResult converts a public ToolResult to the internal representation.
func toInternalResult(r *ToolResult) *mcpserver.ToolResult {
	if r == nil {
		return &mcpserver.ToolResult{}
	}
	result := &mcpserver.ToolResult{
		IsError: r.IsError,
	}
	for _, item := range r.Content {
		result.Content = append(result.Content, mcpserver.ContentItem{
			Type:     item.Type,
			Text:     item.Text,
			MIMEType: item.MIMEType,
			Data:     item.Data,
		})
	}
	return result
}
