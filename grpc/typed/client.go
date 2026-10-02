// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package typed

import (
	"context"
	"fmt"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TypedMCPClient wraps the generated gRPC client stub for the typed Mcp
// service with a clean Go API. It returns simple Go types (ToolInfo,
// ToolResult) instead of proto messages.
//
// Usage:
//
//	client, err := typed.NewTypedMCPClient("localhost:50051")
//	defer client.Close()
//	tools, err := client.ListTools(ctx)
//	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hello"})
type TypedMCPClient struct {
	client mcppb.McpClient
	conn   *grpc.ClientConn
}

// NewTypedMCPClient creates a new typed MCP client connected to the given
// gRPC target. If no transport credentials are provided in opts, insecure
// credentials are used by default.
func NewTypedMCPClient(target string, opts ...grpc.DialOption) (*TypedMCPClient, error) {
	// Always include insecure credentials as a baseline. User-supplied
	// DialOptions are appended on top; if they set explicit credentials,
	// the last-set wins per gRPC semantics.
	allOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	allOpts = append(allOpts, opts...)
	conn, err := grpc.NewClient(target, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial gRPC target %q: %w", target, err)
	}
	return &TypedMCPClient{
		client: mcppb.NewMcpClient(conn),
		conn:   conn,
	}, nil
}

// newTypedMCPClientFromConn creates a typed MCP client from an existing
// gRPC client connection. Used in tests with bufconn.
func newTypedMCPClientFromConn(conn grpc.ClientConnInterface) *TypedMCPClient {
	return &TypedMCPClient{
		client: mcppb.NewMcpClient(conn),
	}
}

// ListTools returns all tools available on the server.
func (c *TypedMCPClient) ListTools(ctx context.Context) ([]*ToolInfo, error) {
	resp, err := c.client.ListTools(ctx, &mcppb.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("ListTools RPC failed: %w", err)
	}
	tools := make([]*ToolInfo, 0, len(resp.GetTools()))
	for _, pt := range resp.GetTools() {
		tools = append(tools, protoToolToInfo(pt))
	}
	return tools, nil
}

// CallTool invokes a tool by name with the given arguments. The arguments
// map is converted to a proto Struct for transmission.
func (c *TypedMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	req := &mcppb.CallToolRequest{
		Name: name,
	}
	if len(args) > 0 {
		s, err := mapToProtoStruct(args)
		if err != nil {
			return nil, fmt.Errorf("converting arguments: %w", err)
		}
		req.Arguments = s
	}
	resp, err := c.client.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CallTool RPC failed: %w", err)
	}
	return protoCallToolResponseToResult(resp), nil
}

// Close closes the underlying gRPC client connection. It is safe to call
// Close multiple times.
func (c *TypedMCPClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
