// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package typed

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/goco-ai/emcp-go/grpc/typed/proto/mcppb"
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

type testEnv struct {
	client     *TypedMCPClient
	grpcServer *grpc.Server
	lis        *bufconn.Listener
	clientConn *grpc.ClientConn
}

func (e *testEnv) cleanup() {
	if e.client != nil {
		_ = e.client.Close()
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

// setupHandler creates a test environment with the given tool configuration.
func setupHandler(t *testing.T, configure func(h *TypedGRPCHandler)) *testEnv {
	t.Helper()
	env := &testEnv{}

	env.lis = bufconn.Listen(bufSize)

	handler := NewTypedGRPCHandler()
	configure(handler)

	env.grpcServer = grpc.NewServer()
	mcppb.RegisterMcpServer(env.grpcServer, handler)
	go func() {
		if err := env.grpcServer.Serve(env.lis); err != nil {
			// Serve returns non-nil when Stop/GracefulStop is called; expected.
		}
	}()

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

	env.client = newTypedMCPClientFromConn(env.clientConn)
	return env
}

// -- Tests --

func TestTyped_ListTools(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "echo",
			Description: "echoes the input",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
			}, nil
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := env.client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", tools[0].Name)
	}
	if tools[0].Description != "echoes the input" {
		t.Errorf("expected description 'echoes the input', got %q", tools[0].Description)
	}
}

func TestTyped_CallTool(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "echo",
			Description: "echoes the input",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error) {
			var args echoArgs
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return nil, fmt.Errorf("unmarshal args: %w", err)
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Msg}},
			}, nil
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.client.CallTool(ctx, "echo", map[string]any{"msg": ""})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "echo: " {
		t.Errorf("expected 'echo: ', got %q", result.Content[0].Text)
	}
}

func TestTyped_CallToolWithArgs(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "echo",
			Description: "echoes the input",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error) {
			var args echoArgs
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return nil, fmt.Errorf("unmarshal args: %w", err)
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Msg}},
			}, nil
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.client.CallTool(ctx, "echo", map[string]any{"msg": "hello world"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "echo: hello world" {
		t.Errorf("expected 'echo: hello world', got %q", result.Content[0].Text)
	}
}

func TestTyped_ToolError(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "fail",
			Description: "always fails",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "something went wrong"}},
			}, nil
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.client.CallTool(ctx, "fail", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", result.Content[0].Text)
	}
}

func TestTyped_CallToolUnknown(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		// No tools registered.
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := env.client.CallTool(ctx, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestTyped_ConcurrentCalls(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "echo",
			Description: "echoes the input",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, argsJSON json.RawMessage) (*mcp.CallToolResult, error) {
			var args echoArgs
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return nil, fmt.Errorf("unmarshal args: %w", err)
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Msg}},
			}, nil
		})
	})
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
			result, err := env.client.CallTool(ctx, "echo", map[string]any{"msg": msg})
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: CallTool failed: %w", i, err)
				return
			}
			if len(result.Content) != 1 {
				errors <- fmt.Errorf("goroutine %d: expected 1 content, got %d", i, len(result.Content))
				return
			}
			expected := "echo: " + msg
			if result.Content[0].Text != expected {
				errors <- fmt.Errorf("goroutine %d: expected %q, got %q", i, expected, result.Content[0].Text)
			}
		}()
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestTyped_Integration(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		// Tool 1: greet with AddToolFunc convenience.
		AddToolFunc(h, &mcp.Tool{
			Name:        "greet",
			Description: "greets a person",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, args greetArgs) (string, error) {
			name := args.Name
			if name == "" {
				name = "stranger"
			}
			return "hello, " + name + "!", nil
		})

		// Tool 2: add with AddToolFunc convenience.
		AddToolFunc(h, &mcp.Tool{
			Name:        "add",
			Description: "adds two numbers",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, args addArgs) (string, error) {
			return fmt.Sprintf("%.0f", args.A+args.B), nil
		})

		// Tool 3: error_tool with raw handler.
		h.AddTool(&mcp.Tool{
			Name:        "error_tool",
			Description: "always returns an error",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "intentional error"}},
			}, nil
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Discover tools.
	tools, err := env.client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{"greet", "add", "error_tool"} {
		if !toolNames[name] {
			t.Errorf("missing tool %q", name)
		}
	}

	// 2. Call greet tool.
	greetResult, err := env.client.CallTool(ctx, "greet", map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("greet CallTool failed: %v", err)
	}
	if len(greetResult.Content) != 1 || greetResult.Content[0].Text != "hello, Alice!" {
		t.Errorf("greet: expected 'hello, Alice!', got %v", greetResult.Content)
	}

	// 3. Call add tool.
	addResult, err := env.client.CallTool(ctx, "add", map[string]any{"a": float64(17), "b": float64(25)})
	if err != nil {
		t.Fatalf("add CallTool failed: %v", err)
	}
	if len(addResult.Content) != 1 || addResult.Content[0].Text != "42" {
		t.Errorf("add: expected '42', got %v", addResult.Content)
	}

	// 4. Call error_tool.
	errResult, err := env.client.CallTool(ctx, "error_tool", nil)
	if err != nil {
		t.Fatalf("error_tool CallTool failed: %v", err)
	}
	if !errResult.IsError {
		t.Error("error_tool: expected IsError=true")
	}
	if len(errResult.Content) != 1 || errResult.Content[0].Text != "intentional error" {
		t.Errorf("error_tool: expected 'intentional error', got %v", errResult.Content)
	}
}

func TestTyped_AddToolFunc_ErrorHandler(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		AddToolFunc(h, &mcp.Tool{
			Name:        "failing",
			Description: "returns an error via AddToolFunc",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, _ struct{}) (string, error) {
			return "", fmt.Errorf("handler error")
		})
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := env.client.CallTool(ctx, "failing", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for handler error")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "handler error" {
		t.Errorf("expected 'handler error', got %v", result.Content)
	}
}

func TestTyped_RemoveTools(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		h.AddTool(&mcp.Tool{
			Name:        "temp",
			Description: "temporary tool",
			InputSchema: map[string]any{"type": "object"},
		}, func(_ context.Context, _ json.RawMessage) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
			}, nil
		})
		h.RemoveTools("temp")
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := env.client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools after removal, got %d", len(tools))
	}
}

func TestTyped_ListToolsEmpty(t *testing.T) {
	env := setupHandler(t, func(h *TypedGRPCHandler) {
		// No tools registered.
	})
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := env.client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
