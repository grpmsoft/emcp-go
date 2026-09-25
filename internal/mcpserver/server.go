// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

// Package mcpserver implements the MCP server logic. It is internal to
// emcp-go; external consumers use the thin wrapper in the root emcp package.
package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	emcpgrpc "github.com/goco-ai/emcp-go/grpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolHandler is the public handler signature used by consumers. It receives
// the tool name, decoded arguments, and returns a result or error.
// This type mirrors emcp.ToolHandler but is defined here to avoid a circular
// import between the root package and internal.
type ToolHandler func(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

// ToolResult is the internal representation of a tool call result.
// The root emcp package converts between this and its own public ToolResult.
type ToolResult struct {
	Content []ContentItem
	IsError bool
}

// ContentItem is the internal representation of a content item.
type ContentItem struct {
	Type     string
	Text     string
	MIMEType string
	Data     []byte
}

// Config holds parameters for creating a Server.
type Config struct {
	Name         string
	Version      string
	Instructions string
}

// Server wraps an MCP server and provides a clean internal API for adding
// tools and creating transport handlers. It delegates all MCP protocol work
// to the official SDK.
type Server struct {
	mcpServer *mcp.Server

	mu          sync.Mutex
	grpcHandler *emcpgrpc.GRPCHandler
}

// New creates a new MCP server with the given configuration.
func New(cfg Config) *Server {
	name := cfg.Name
	if name == "" {
		name = "emcp-server"
	}
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}

	var opts *mcp.ServerOptions
	if cfg.Instructions != "" {
		opts = &mcp.ServerOptions{
			Instructions: cfg.Instructions,
		}
	}

	return &Server{
		mcpServer: mcp.NewServer(
			&mcp.Implementation{Name: name, Version: version},
			opts,
		),
	}
}

// AddTool registers a tool with the given handler. The inputSchema is passed
// through to the SDK as-is (it must be a valid JSON Schema object, typically
// map[string]any).
func (s *Server) AddTool(name, description string, inputSchema map[string]any, handler ToolHandler) {
	// The SDK requires InputSchema to be non-nil. When the caller passes nil
	// (tool accepts no arguments), use a minimal empty-object schema.
	schema := any(inputSchema)
	if inputSchema == nil {
		schema = map[string]any{"type": "object"}
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: schema,
	}

	// Use the low-level Server.AddTool (non-generic) which accepts mcp.ToolHandler.
	// This avoids the generic AddTool which requires typed structs for input.
	s.mcpServer.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments from the raw request params.
		args, err := extractArguments(req)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "invalid arguments: " + err.Error()}},
			}, nil
		}

		result, err := handler(ctx, name, args)
		if err != nil {
			return nil, err
		}

		return toSDKResult(result), nil
	})
}

// HTTPHandler returns an http.Handler that serves MCP over Streamable HTTP.
// Mount this on your HTTP mux: mux.Handle("/mcp", srv.HTTPHandler())
func (s *Server) HTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return s.mcpServer },
		nil,
	)
}

// GRPCHandler returns the gRPC handler for registering with a grpc.Server.
// The handler is created lazily on first call and reused thereafter.
func (s *Server) GRPCHandler() *emcpgrpc.GRPCHandler {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.grpcHandler == nil {
		s.grpcHandler = emcpgrpc.NewGRPCHandler(func() *mcp.Server { return s.mcpServer })
	}
	return s.grpcHandler
}

// MCPServer returns the underlying mcp.Server for advanced use cases.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}

// extractArguments pulls the arguments map from a CallToolRequest.
// The SDK's CallToolRequest wraps CallToolParamsRaw, which stores arguments
// as json.RawMessage. We unmarshal that into map[string]any.
func extractArguments(req *mcp.CallToolRequest) (map[string]any, error) {
	if req == nil || req.Params == nil {
		return nil, nil
	}

	raw := req.Params.Arguments
	if raw == nil {
		return nil, nil
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return args, nil
}

// toSDKResult converts our internal ToolResult to the SDK's CallToolResult.
func toSDKResult(r *ToolResult) *mcp.CallToolResult {
	if r == nil {
		return &mcp.CallToolResult{}
	}

	result := &mcp.CallToolResult{
		IsError: r.IsError,
	}
	for _, item := range r.Content {
		result.Content = append(result.Content, toSDKContent(item))
	}
	return result
}

// toSDKContent converts an internal ContentItem to an SDK Content value.
func toSDKContent(item ContentItem) mcp.Content {
	switch item.Type {
	case "image":
		return &mcp.ImageContent{
			Data:     item.Data,
			MIMEType: item.MIMEType,
		}
	case "audio":
		return &mcp.AudioContent{
			Data:     item.Data,
			MIMEType: item.MIMEType,
		}
	default:
		return &mcp.TextContent{Text: item.Text}
	}
}
