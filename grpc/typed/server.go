// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Package typed provides a typed gRPC transport for MCP. Unlike the
// bidirectional stream transport in the parent grpc package, the typed
// transport exposes each MCP method as its own gRPC RPC with native
// protobuf messages (based on the Google canonical proto for MCP).
//
// The server adapter (TypedGRPCHandler) maintains its own tool registry
// and implements the mcppb.McpServer interface (all 8 RPCs from the
// Google canonical proto). Tools are registered via AddTool and invoked
// through the CallTool RPC. Non-tool RPCs (resources, prompts,
// completions) return codes.Unimplemented until bridging is wired up.
//
// The client (TypedMCPClient) wraps the generated gRPC stub with a clean
// Go API that returns simple Go types instead of proto messages.
//
// Both transports (stream and typed) can be registered on the same
// grpc.Server since they are different gRPC services.
package typed

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToolHandlerFunc is the handler function signature for typed gRPC tools.
// It receives the context and raw JSON arguments, and returns an
// mcp.CallToolResult. Errors returned from the handler are treated as
// gRPC-level errors (codes.Internal). Tool-level errors should be reported
// via CallToolResult.IsError.
type ToolHandlerFunc func(ctx context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error)

// registeredTool pairs a tool definition with its handler.
type registeredTool struct {
	tool    *mcp.Tool
	handler ToolHandlerFunc
}

// TypedGRPCHandler implements the Google canonical mcppb.McpServer gRPC
// service (all 8 RPCs). It maintains its own tool registry and dispatches
// CallTool RPCs to the registered handlers. Non-tool RPCs (resources,
// prompts, completions) return codes.Unimplemented.
//
// Usage:
//
//	handler := typed.NewTypedGRPCHandler()
//	handler.AddTool(&mcp.Tool{Name: "echo", ...}, echoHandler)
//	grpcServer := grpc.NewServer()
//	mcppb.RegisterMcpServer(grpcServer, handler)
type TypedGRPCHandler struct {
	mcppb.UnimplementedMcpServer

	mu    sync.RWMutex
	tools map[string]*registeredTool

	// getServer returns the shared MCP server for future bridging.
	// It is currently stored but not used for tool dispatch (the local
	// registry is used instead). When non-nil, it enables future
	// integration where tools registered via emcp.Server.AddTool become
	// visible through the typed transport.
	getServer func() *mcp.Server
}

// Compile-time check: TypedGRPCHandler implements mcppb.McpServer.
var _ mcppb.McpServer = (*TypedGRPCHandler)(nil)

// NewTypedGRPCHandler creates a new TypedGRPCHandler with an empty tool
// registry. An optional getServer function can be provided to associate
// the handler with a shared *mcp.Server for future bridging. Pass nil
// for standalone usage.
func NewTypedGRPCHandler(opts ...func(*TypedGRPCHandler)) *TypedGRPCHandler {
	h := &TypedGRPCHandler{
		tools: make(map[string]*registeredTool),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithMCPServer returns an option that associates the handler with a
// shared *mcp.Server via a factory function.
func WithMCPServer(getServer func() *mcp.Server) func(*TypedGRPCHandler) {
	return func(h *TypedGRPCHandler) {
		h.getServer = getServer
	}
}

// AddTool registers a tool with the handler. If a tool with the same name
// already exists, it is replaced. The handler receives raw JSON arguments
// and must return an mcp.CallToolResult.
func (h *TypedGRPCHandler) AddTool(tool *mcp.Tool, handler ToolHandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools[tool.Name] = &registeredTool{
		tool:    tool,
		handler: handler,
	}
}

// RemoveTools removes tools by name. It is not an error to remove a
// nonexistent tool.
func (h *TypedGRPCHandler) RemoveTools(names ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, name := range names {
		delete(h.tools, name)
	}
}

// --- Tool RPCs (implemented) ---

// ListTools implements mcppb.McpServer. It returns all registered tools
// as proto Tool messages, sorted by name for deterministic output.
func (h *TypedGRPCHandler) ListTools(_ context.Context, _ *mcppb.ListToolsRequest) (*mcppb.ListToolsResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Collect names and sort for deterministic iteration order.
	names := make([]string, 0, len(h.tools))
	for name := range h.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	resp := &mcppb.ListToolsResponse{
		Tools: make([]*mcppb.Tool, 0, len(h.tools)),
	}
	for _, name := range names {
		rt := h.tools[name]
		pt, err := mcpToolToProto(rt.tool)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting tool %q: %v", rt.tool.Name, err)
		}
		resp.Tools = append(resp.Tools, pt)
	}
	return resp, nil
}

// CallTool implements mcppb.McpServer. It dispatches the RPC to the
// registered tool handler, converting proto arguments to JSON and the
// result back to proto.
func (h *TypedGRPCHandler) CallTool(ctx context.Context, req *mcppb.CallToolRequest) (*mcppb.CallToolResponse, error) {
	name, argsJSON, err := protoCallToolArgs(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	h.mu.RLock()
	rt, ok := h.tools[name]
	h.mu.RUnlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown tool %q", name)
	}

	result, err := rt.handler(ctx, argsJSON)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "tool %q failed: %v", name, err)
	}
	if result == nil {
		return nil, status.Errorf(codes.Internal, "tool %q returned nil result", name)
	}

	return callToolResultToProto(result), nil
}

// --- Resource RPCs (unimplemented) ---

// ListResources implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) ListResources(_ context.Context, _ *mcppb.ListResourcesRequest) (*mcppb.ListResourcesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListResources not implemented in typed transport")
}

// ReadResource implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) ReadResource(_ context.Context, _ *mcppb.ReadResourceRequest) (*mcppb.ReadResourceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ReadResource not implemented in typed transport")
}

// ListResourceTemplates implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) ListResourceTemplates(_ context.Context, _ *mcppb.ListResourceTemplatesRequest) (*mcppb.ListResourceTemplatesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListResourceTemplates not implemented in typed transport")
}

// --- Prompt RPCs (unimplemented) ---

// ListPrompts implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) ListPrompts(_ context.Context, _ *mcppb.ListPromptsRequest) (*mcppb.ListPromptsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListPrompts not implemented in typed transport")
}

// GetPrompt implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) GetPrompt(_ context.Context, _ *mcppb.GetPromptRequest) (*mcppb.GetPromptResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetPrompt not implemented in typed transport")
}

// --- Completion RPC (unimplemented) ---

// Complete implements mcppb.McpServer. Currently returns Unimplemented.
func (h *TypedGRPCHandler) Complete(_ context.Context, _ *mcppb.CompletionRequest) (*mcppb.CompletionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Complete not implemented in typed transport")
}

// --- Convenience helpers ---

// AddToolFunc is a convenience method that registers a tool with a typed
// handler function. The arguments type In is deserialized from JSON
// automatically. The handler returns a text result string and an error.
// If the handler returns an error, it is reported as a tool-level error
// (IsError=true) with the error message as text content.
func AddToolFunc[In any](h *TypedGRPCHandler, tool *mcp.Tool, fn func(ctx context.Context, args In) (string, error)) {
	h.AddTool(tool, func(ctx context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error) {
		var args In
		if len(argsJSON) > 0 {
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return nil, fmt.Errorf("unmarshaling arguments: %w", err)
			}
		}
		text, err := fn(ctx, args)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil
	})
}
