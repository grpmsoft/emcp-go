// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package emcp

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	emcpv1 "github.com/goco-ai/emcp-go/grpc/proto/emcpv1"
	"google.golang.org/grpc"
)

// -- Server test infrastructure --

// newTestServer creates an emcp.Server with the standard test tools (echo,
// greet, add, error_tool) and returns it.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	srv := NewServer(ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
	})

	srv.AddTool("echo", "echoes the input", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		msg, _ := args["msg"].(string)
		return TextResult("echo: " + msg), nil
	})

	srv.AddTool("greet", "greets a person", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		name, _ := args["name"].(string)
		if name == "" {
			name = "stranger"
		}
		return TextResult("hello, " + name + "!"), nil
	})

	srv.AddTool("add", "adds two numbers", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number"},
			"b": map[string]any{"type": "number"},
		},
	}, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return TextResult(fmt.Sprintf("%.0f", a+b)), nil
	})

	srv.AddTool("error_tool", "always returns an error", nil,
		func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
			return ErrorResult("intentional error"), nil
		},
	)

	return srv
}

// newHTTPServerAndClient creates an httptest.Server from the emcp.Server's
// HTTPHandler and connects an emcp.Client to it. Returns the client and a
// cleanup function.
func newHTTPServerAndClient(t *testing.T, srv *Server) (*Client, func()) {
	t.Helper()

	ts := httptest.NewServer(srv.HTTPHandler())

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		ts.Close()
		t.Fatalf("NewClient failed: %v", err)
	}

	return client, func() {
		client.Close()
		ts.Close()
	}
}

// newGRPCServerAndClient creates a gRPC server on a TCP listener with the
// emcp.Server's GRPCHandler and connects an emcp.Client to it via gRPC.
// Returns the client and a cleanup function.
func newGRPCServerAndClient(t *testing.T, srv *Server) (*Client, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	emcpv1.RegisterMCPTransportServer(grpcServer, srv.GRPCHandler())
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	client, err := NewClient(Config{
		Endpoint:  lis.Addr().String(),
		Transport: TransportGRPC,
	})
	if err != nil {
		grpcServer.Stop()
		_ = lis.Close()
		t.Fatalf("NewClient gRPC failed: %v", err)
	}

	return client, func() {
		client.Close()
		grpcServer.Stop()
		_ = lis.Close()
	}
}

// -- HTTP Tests --

func TestServer_AddToolAndListViaHTTP(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "list-test", Version: "0.1.0"})
	srv.AddTool("ping", "returns pong", nil,
		func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
			return TextResult("pong"), nil
		},
	)

	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "ping" {
		t.Errorf("expected tool name 'ping', got %q", tools[0].Name)
	}
	if tools[0].Description != "returns pong" {
		t.Errorf("expected description 'returns pong', got %q", tools[0].Description)
	}
}

func TestServer_CallToolViaHTTP(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hello server"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "echo: hello server" {
		t.Errorf("expected 'echo: hello server', got %q", result.Content[0].Text)
	}
}

func TestServer_HTTPHandler(t *testing.T) {
	srv := newTestServer(t)

	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify we can list and call tools through the HTTP handler.
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	result, err := client.CallTool(ctx, "greet", map[string]any{"name": "Bob"})
	if err != nil {
		t.Fatalf("CallTool greet failed: %v", err)
	}
	if result.Content[0].Text != "hello, Bob!" {
		t.Errorf("expected 'hello, Bob!', got %q", result.Content[0].Text)
	}
}

func TestServer_ToolError(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "error_tool", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "intentional error" {
		t.Errorf("expected 'intentional error', got %q", result.Content[0].Text)
	}
}

func TestServer_NilArgs(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "nil-args", Version: "0.1.0"})
	srv.AddTool("no_args", "tool with no arguments", nil,
		func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
			if args == nil {
				return TextResult("nil args"), nil
			}
			return TextResult(fmt.Sprintf("got %d args", len(args))), nil
		},
	)

	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "no_args", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	// When no args are sent, the handler should receive nil.
	if result.Content[0].Text != "nil args" {
		t.Errorf("expected 'nil args', got %q", result.Content[0].Text)
	}
}

func TestServer_Ping(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestServer_Close(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "close-test", Version: "0.1.0"})

	// Close should be safe even without any tools or connections.
	if err := srv.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should be safe.
	if err := srv.Close(); err != nil {
		t.Fatalf("Double Close failed: %v", err)
	}
}

func TestServer_Internal(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "internal-test", Version: "0.1.0"})

	internal := srv.Internal()
	if internal == nil {
		t.Fatal("Internal() returned nil")
	}
}

func TestServer_ConcurrentHTTP(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			msg := fmt.Sprintf("concurrent-%d", i)
			result, err := client.CallTool(ctx, "echo", map[string]any{"msg": msg})
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: CallTool failed: %w", i, err)
				return
			}
			if len(result.Content) != 1 {
				errs <- fmt.Errorf("goroutine %d: expected 1 content, got %d", i, len(result.Content))
				return
			}
			expected := "echo: " + msg
			if result.Content[0].Text != expected {
				errs <- fmt.Errorf("goroutine %d: expected %q, got %q", i, expected, result.Content[0].Text)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// -- gRPC Tests --

func TestServer_GRPCHandler(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newGRPCServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools via gRPC failed: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "via grpc"})
	if err != nil {
		t.Fatalf("CallTool via gRPC failed: %v", err)
	}
	if result.Content[0].Text != "echo: via grpc" {
		t.Errorf("expected 'echo: via grpc', got %q", result.Content[0].Text)
	}
}

func TestServer_GRPCPing(t *testing.T) {
	srv := newTestServer(t)
	client, cleanup := newGRPCServerAndClient(t, srv)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping via gRPC failed: %v", err)
	}
}

// -- Integration Tests --

func TestServer_Integration(t *testing.T) {
	srv := newTestServer(t)

	// Test both transports against the same server.
	t.Run("HTTP", func(t *testing.T) {
		client, cleanup := newHTTPServerAndClient(t, srv)
		defer cleanup()
		runIntegrationScenario(t, client)
	})

	t.Run("gRPC", func(t *testing.T) {
		client, cleanup := newGRPCServerAndClient(t, srv)
		defer cleanup()
		runIntegrationScenario(t, client)
	})
}

// runIntegrationScenario exercises the full MCP lifecycle: discover tools,
// call each tool, verify results, and ping.
func runIntegrationScenario(t *testing.T, client *Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Discover tools.
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{"echo", "greet", "add", "error_tool"} {
		if !toolNames[name] {
			t.Errorf("missing tool %q", name)
		}
	}

	// 2. Call echo.
	echoResult, err := client.CallTool(ctx, "echo", map[string]any{"msg": "integration"})
	if err != nil {
		t.Fatalf("echo CallTool failed: %v", err)
	}
	if echoResult.Content[0].Text != "echo: integration" {
		t.Errorf("echo: expected 'echo: integration', got %q", echoResult.Content[0].Text)
	}

	// 3. Call greet.
	greetResult, err := client.CallTool(ctx, "greet", map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("greet CallTool failed: %v", err)
	}
	if greetResult.Content[0].Text != "hello, Alice!" {
		t.Errorf("greet: expected 'hello, Alice!', got %q", greetResult.Content[0].Text)
	}

	// 4. Call add.
	addResult, err := client.CallTool(ctx, "add", map[string]any{"a": float64(17), "b": float64(25)})
	if err != nil {
		t.Fatalf("add CallTool failed: %v", err)
	}
	if addResult.Content[0].Text != "42" {
		t.Errorf("add: expected '42', got %q", addResult.Content[0].Text)
	}

	// 5. Call error_tool.
	errResult, err := client.CallTool(ctx, "error_tool", nil)
	if err != nil {
		t.Fatalf("error_tool CallTool failed: %v", err)
	}
	if !errResult.IsError {
		t.Error("error_tool: expected IsError=true")
	}
	if errResult.Content[0].Text != "intentional error" {
		t.Errorf("error_tool: expected 'intentional error', got %q", errResult.Content[0].Text)
	}

	// 6. Ping after all calls.
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("final Ping failed: %v", err)
	}
}

// -- Convenience constructor tests --

func TestTextResult(t *testing.T) {
	r := TextResult("hello")
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(r.Content))
	}
	if r.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", r.Content[0].Type)
	}
	if r.Content[0].Text != "hello" {
		t.Errorf("expected 'hello', got %q", r.Content[0].Text)
	}
	if r.IsError {
		t.Error("expected IsError=false")
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("something failed")
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(r.Content))
	}
	if r.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", r.Content[0].Type)
	}
	if r.Content[0].Text != "something failed" {
		t.Errorf("expected 'something failed', got %q", r.Content[0].Text)
	}
	if !r.IsError {
		t.Error("expected IsError=true")
	}
}
