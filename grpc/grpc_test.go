// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	emcpv1 "github.com/goco-ai/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// -- Typed tool argument structs --

type echoArgs struct {
	Msg string `json:"msg"`
}

type greetArgs struct {
	Name string `json:"name"`
}

type addArgs struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

// -- Test infrastructure --

// testEnv holds all resources for a single test scenario.
type testEnv struct {
	session    *mcp.ClientSession
	grpcServer *grpc.Server
	lis        *bufconn.Listener
	clientConn *grpc.ClientConn
}

func (e *testEnv) cleanup() {
	if e.session != nil {
		_ = e.session.Close()
	}
	if e.grpcServer != nil {
		e.grpcServer.Stop()
	}
	if e.clientConn != nil {
		_ = e.clientConn.Close()
	}
	if e.lis != nil {
		_ = e.lis.Close()
	}
}

// setupEchoServer creates a test environment with a single "echo" tool.
func setupEchoServer(t *testing.T) *testEnv {
	t.Helper()
	return setupServer(t, func(s *mcp.Server) {
		mcp.AddTool(s,
			&mcp.Tool{
				Name:        "echo",
				Description: "echoes the input",
			},
			func(_ context.Context, _ *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Msg}},
				}, nil, nil
			},
		)
	})
}

// setupServer creates a test environment with the given tool configuration.
func setupServer(t *testing.T, configure func(s *mcp.Server)) *testEnv {
	t.Helper()
	env := &testEnv{}

	env.lis = bufconn.Listen(bufSize)

	// Create MCP server.
	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	configure(mcpServer)

	// Create and start gRPC server.
	env.grpcServer = grpc.NewServer()
	handler := NewGRPCHandler(func() *mcp.Server { return mcpServer })
	emcpv1.RegisterMCPTransportServer(env.grpcServer, handler)
	go func() {
		if err := env.grpcServer.Serve(env.lis); err != nil {
			// Serve returns non-nil when Stop/GracefulStop is called; expected.
		}
	}()

	// Create gRPC client connection via bufconn.
	var err error
	env.clientConn, err = grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return env.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}

	// Create MCP client over gRPC transport.
	transport := &GRPCTransport{conn: env.clientConn}
	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "0.1.0"},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env.session, err = client.Connect(ctx, transport, nil)
	if err != nil {
		env.cleanup()
		t.Fatalf("failed to connect MCP client: %v", err)
	}

	return env
}

// -- Tests --

// TestGRPC_ConnectAndPing verifies that a client can connect and ping the server.
func TestGRPC_ConnectAndPing(t *testing.T) {
	env := setupEchoServer(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := env.session.Ping(ctx, nil); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// TestGRPC_ListTools verifies that the client can discover server tools.
func TestGRPC_ListTools(t *testing.T) {
	env := setupEchoServer(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", result.Tools[0].Name)
	}
}

// TestGRPC_CallTool verifies basic tool invocation with an empty argument.
func TestGRPC_CallTool(t *testing.T) {
	env := setupEchoServer(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": ""},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "echo: " {
		t.Errorf("expected 'echo: ', got %q", tc.Text)
	}
}

// TestGRPC_CallToolWithArgs verifies tool invocation with arguments.
func TestGRPC_CallToolWithArgs(t *testing.T) {
	env := setupEchoServer(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "hello world"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "echo: hello world" {
		t.Errorf("expected 'echo: hello world', got %q", tc.Text)
	}
}

// TestGRPC_ToolError verifies that tool errors are properly propagated.
func TestGRPC_ToolError(t *testing.T) {
	env := setupServer(t, func(s *mcp.Server) {
		mcp.AddTool(s,
			&mcp.Tool{
				Name:        "fail",
				Description: "always fails",
			},
			func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "something went wrong"}},
				}, nil, nil
			},
		)
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.session.CallTool(ctx, &mcp.CallToolParams{Name: "fail"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", tc.Text)
	}
}

// TestGRPC_ConcurrentCalls verifies that 10 goroutines can call tools
// simultaneously without races or errors.
func TestGRPC_ConcurrentCalls(t *testing.T) {
	env := setupEchoServer(t)
	defer env.cleanup()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			msg := fmt.Sprintf("concurrent-%d", i)
			result, err := env.session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "echo",
				Arguments: map[string]any{"msg": msg},
			})
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: CallTool failed: %w", i, err)
				return
			}
			if len(result.Content) != 1 {
				errors <- fmt.Errorf("goroutine %d: expected 1 content, got %d", i, len(result.Content))
				return
			}
			tc, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				errors <- fmt.Errorf("goroutine %d: expected TextContent, got %T", i, result.Content[0])
				return
			}
			expected := "echo: " + msg
			if tc.Text != expected {
				errors <- fmt.Errorf("goroutine %d: expected %q, got %q", i, expected, tc.Text)
			}
		}()
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

// TestGRPC_StreamClose verifies that closing the client session cleanly
// terminates the connection.
func TestGRPC_StreamClose(t *testing.T) {
	env := setupEchoServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify connection works before closing.
	if err := env.session.Ping(ctx, nil); err != nil {
		t.Fatalf("Ping before close failed: %v", err)
	}

	// Close the session.
	if err := env.session.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, operations should fail.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	err := env.session.Ping(ctx2, nil)
	if err == nil {
		t.Error("expected error after Close, got nil")
	}

	// Clean up remaining resources (session already closed).
	env.session = nil
	env.cleanup()
}

// TestGRPC_ServerShutdown verifies that stopping the gRPC server causes
// the client to receive errors.
func TestGRPC_ServerShutdown(t *testing.T) {
	env := setupEchoServer(t)
	defer func() {
		// Session may already be broken; just clean up what we can.
		env.session = nil
		env.grpcServer = nil // already stopped
		env.cleanup()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify connection works.
	if err := env.session.Ping(ctx, nil); err != nil {
		t.Fatalf("Ping before shutdown failed: %v", err)
	}

	// Stop the gRPC server.
	env.grpcServer.Stop()

	// Give a moment for the stream to detect the shutdown.
	time.Sleep(100 * time.Millisecond)

	// Calls after shutdown should fail.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	err := env.session.Ping(ctx2, nil)
	if err == nil {
		t.Error("expected error after server shutdown, got nil")
	}
}

// TestGRPC_Integration is a full end-to-end test with 3 tools exercising
// the complete MCP lifecycle over gRPC: connect, discover, call, close.
func TestGRPC_Integration(t *testing.T) {
	env := setupServer(t, func(s *mcp.Server) {
		// Tool 1: greet — simple text response.
		mcp.AddTool(s,
			&mcp.Tool{
				Name:        "greet",
				Description: "greets a person",
			},
			func(_ context.Context, _ *mcp.CallToolRequest, args greetArgs) (*mcp.CallToolResult, any, error) {
				name := args.Name
				if name == "" {
					name = "stranger"
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "hello, " + name + "!"}},
				}, nil, nil
			},
		)

		// Tool 2: add — arithmetic.
		mcp.AddTool(s,
			&mcp.Tool{
				Name:        "add",
				Description: "adds two numbers",
			},
			func(_ context.Context, _ *mcp.CallToolRequest, args addArgs) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%.0f", args.A+args.B)}},
				}, nil, nil
			},
		)

		// Tool 3: error_tool — returns a tool error.
		mcp.AddTool(s,
			&mcp.Tool{
				Name:        "error_tool",
				Description: "always returns an error",
			},
			func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "intentional error"}},
				}, nil, nil
			},
		)
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Discover tools via iterator.
	toolNames := make(map[string]bool)
	for tool, err := range env.session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools iterator error: %v", err)
		}
		toolNames[tool.Name] = true
	}
	if len(toolNames) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(toolNames), toolNames)
	}
	for _, name := range []string{"greet", "add", "error_tool"} {
		if !toolNames[name] {
			t.Errorf("missing tool %q", name)
		}
	}

	// 2. Call greet tool.
	greetResult, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("greet CallTool failed: %v", err)
	}
	tc, ok := greetResult.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "hello, Alice!" {
		t.Errorf("greet: expected 'hello, Alice!', got %v", greetResult.Content)
	}

	// 3. Call add tool.
	addResult, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add",
		Arguments: map[string]any{"a": float64(17), "b": float64(25)},
	})
	if err != nil {
		t.Fatalf("add CallTool failed: %v", err)
	}
	tc, ok = addResult.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "42" {
		t.Errorf("add: expected '42', got %v", addResult.Content)
	}

	// 4. Call error_tool.
	errResult, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name: "error_tool",
	})
	if err != nil {
		t.Fatalf("error_tool CallTool failed: %v", err)
	}
	if !errResult.IsError {
		t.Error("error_tool: expected IsError=true")
	}
	tc, ok = errResult.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "intentional error" {
		t.Errorf("error_tool: expected 'intentional error', got %v", errResult.Content)
	}

	// 5. Ping to confirm session is still healthy after all calls.
	if err := env.session.Ping(ctx, nil); err != nil {
		t.Fatalf("final Ping failed: %v", err)
	}
}
