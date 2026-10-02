// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Package mcpserver implements the MCP server logic. It is internal to
// emcp-go; external consumers use the thin wrapper in the root emcp package.
package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	emcpgrpc "github.com/grpmsoft/emcp-go/grpc"
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
	Content           []ContentItem
	StructuredContent map[string]any
	IsError           bool
}

// ContentItem is the internal representation of a content item.
// Supports all MCP content types: text, image, audio, resource_link,
// and resource (embedded resource).
type ContentItem struct {
	Type     string
	Text     string
	MIMEType string
	Data     []byte

	// resource_link fields
	URI         string
	Name        string
	Title       string
	Description string

	// embedded resource
	Resource *ResourceContents
}

// ResourceContents holds the contents of an embedded resource.
type ResourceContents struct {
	URI      string
	MIMEType string
	Text     string
	Blob     []byte
}

// Config holds parameters for creating a Server.
type Config struct {
	Name         string
	Version      string
	Instructions string

	// Stateless enables stateless HTTP mode (MCP 2026-07-28 spec).
	// In stateless mode, no Mcp-Session-Id header is used and each request
	// gets a temporary session. This is the correct mode for short-lived
	// CLI clients (like GODE daemon consumers). Defaults to true.
	Stateless bool

	// SessionTimeout configures how long idle sessions survive before
	// automatic cleanup. Zero means never expire (leaks sessions).
	SessionTimeout time.Duration
}

// Server wraps an MCP server and provides a clean internal API for adding
// tools and creating transport handlers. It delegates all MCP protocol work
// to the official SDK.
type Server struct {
	mcpServer *mcp.Server
	config    Config

	mu          sync.Mutex
	httpHandler http.Handler
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
		config: cfg,
	}
}

// AddTool registers a tool with the given handler. The inputSchema is passed
// through to the SDK as-is (it must be a valid JSON Schema object, typically
// map[string]any). If the schema is nil or missing "type", it is normalized
// to {"type": "object"} to prevent SDK panics.
func (s *Server) AddTool(name, description string, inputSchema map[string]any, handler ToolHandler) {
	// The SDK requires InputSchema to have "type": "object". When the caller
	// passes nil or a schema without "type", normalize it to prevent panics.
	schema := normalizeSchema(inputSchema)

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
// The handler is created lazily on first call and reused thereafter, ensuring
// that session state is preserved across HTTP requests from the same client.
// Mount this on your HTTP mux: mux.Handle("/mcp", srv.HTTPHandler())
func (s *Server) HTTPHandler() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpHandler == nil {
		opts := &mcp.StreamableHTTPOptions{
			Stateless:      s.config.Stateless,
			SessionTimeout: s.config.SessionTimeout,
		}
		s.httpHandler = mcp.NewStreamableHTTPHandler(
			func(_ *http.Request) *mcp.Server { return s.mcpServer },
			opts,
		)
	}
	return s.httpHandler
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
// B7 fix: preserves StructuredContent through the conversion.
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
	// Preserve StructuredContent (SEP-2106) if present.
	if r.StructuredContent != nil {
		result.StructuredContent = r.StructuredContent
	}
	return result
}

// toSDKContent converts an internal ContentItem to an SDK Content value.
// B7 fix: handles resource_link and embedded resource types, not just
// text/image/audio.
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
	case "resource_link":
		return &mcp.ResourceLink{
			URI:         item.URI,
			Name:        item.Name,
			Title:       item.Title,
			Description: item.Description,
			MIMEType:    item.MIMEType,
		}
	case "resource":
		er := &mcp.EmbeddedResource{}
		if item.Resource != nil {
			er.Resource = &mcp.ResourceContents{
				URI:      item.Resource.URI,
				MIMEType: item.Resource.MIMEType,
				Text:     item.Resource.Text,
				Blob:     item.Resource.Blob,
			}
		}
		return er
	default:
		return &mcp.TextContent{Text: item.Text}
	}
}

// normalizeSchema ensures the input schema has "type": "object" set.
// The SDK panics if the schema is nil or missing the "type" field.
func normalizeSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}
	if _, hasType := schema["type"]; !hasType {
		// Copy the map to avoid mutating the caller's original.
		normalized := make(map[string]any, len(schema)+1)
		for k, v := range schema {
			normalized[k] = v
		}
		normalized["type"] = "object"
		return normalized
	}
	return schema
}
