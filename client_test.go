// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	emcpgrpc "github.com/goco-ai/emcp-go/grpc"
	emcpv1 "github.com/goco-ai/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

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

// newHTTPTestServer creates an MCP server with the given tools and returns
// an httptest.Server serving it via StreamableHTTPHandler.
func newHTTPTestServer(t *testing.T, configure func(s *mcp.Server)) *httptest.Server {
	t.Helper()

	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	configure(mcpServer)

	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return mcpServer },
		nil,
	)

	return httptest.NewServer(handler)
}

// newEchoHTTPServer creates a test HTTP server with a single "echo" tool.
func newEchoHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newHTTPTestServer(t, func(s *mcp.Server) {
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

// -- HTTP Transport Tests --

func TestClient_HTTP_ListTools(t *testing.T) {
	ts := newEchoHTTPServer(t)
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

	tools, err := client.ListTools(ctx)
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

func TestClient_HTTP_CallTool(t *testing.T) {
	ts := newEchoHTTPServer(t)
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

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hello world"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "echo: hello world" {
		t.Errorf("expected 'echo: hello world', got %q", result.Content[0].Text)
	}
}

func TestClient_HTTP_CallToolEmpty(t *testing.T) {
	ts := newEchoHTTPServer(t)
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

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": ""})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "echo: " {
		t.Errorf("expected 'echo: ', got %q", result.Content[0].Text)
	}
}

func TestClient_HTTP_ToolError(t *testing.T) {
	ts := newHTTPTestServer(t, func(s *mcp.Server) {
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

	result, err := client.CallTool(ctx, "fail", nil)
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

func TestClient_HTTP_Ping(t *testing.T) {
	ts := newEchoHTTPServer(t)
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

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestClient_HTTP_Close(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Close should succeed.
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should be safe.
	if err := client.Close(); err != nil {
		t.Fatalf("Double Close failed: %v", err)
	}

	// Operations after close should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.ListTools(ctx)
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestClient_HTTP_ConcurrentCalls(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

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

func TestClient_HTTP_Integration(t *testing.T) {
	ts := newHTTPTestServer(t, func(s *mcp.Server) {
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
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Discover tools.
	tools, err := client.ListTools(ctx)
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

	// 2. Call greet.
	greetResult, err := client.CallTool(ctx, "greet", map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("greet CallTool failed: %v", err)
	}
	if len(greetResult.Content) != 1 || greetResult.Content[0].Text != "hello, Alice!" {
		t.Errorf("greet: expected 'hello, Alice!', got %v", greetResult.Content)
	}

	// 3. Call add.
	addResult, err := client.CallTool(ctx, "add", map[string]any{"a": float64(17), "b": float64(25)})
	if err != nil {
		t.Fatalf("add CallTool failed: %v", err)
	}
	if len(addResult.Content) != 1 || addResult.Content[0].Text != "42" {
		t.Errorf("add: expected '42', got %v", addResult.Content)
	}

	// 4. Call error_tool.
	errResult, err := client.CallTool(ctx, "error_tool", nil)
	if err != nil {
		t.Fatalf("error_tool CallTool failed: %v", err)
	}
	if !errResult.IsError {
		t.Error("error_tool: expected IsError=true")
	}
	if len(errResult.Content) != 1 || errResult.Content[0].Text != "intentional error" {
		t.Errorf("error_tool: expected 'intentional error', got %v", errResult.Content)
	}

	// 5. Ping after all calls.
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("final Ping failed: %v", err)
	}
}

func TestClient_HTTP_BearerToken(t *testing.T) {
	// Track whether the Authorization header was sent.
	var mu sync.Mutex
	var gotAuth string

	// Create MCP server once so session state persists across requests.
	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	mcp.AddTool(mcpServer,
		&mcp.Tool{Name: "noop", Description: "does nothing"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
			}, nil, nil
		},
	)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return mcpServer },
		nil,
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		mcpHandler.ServeHTTP(w, r)
	}))
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:    ts.URL,
		Transport:   TransportHTTP,
		BearerToken: "test-secret-token",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	mu.Lock()
	auth := gotAuth
	mu.Unlock()

	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("expected Authorization header with 'Bearer ' prefix, got %q", auth)
	}
	if auth != "Bearer test-secret-token" {
		t.Errorf("expected 'Bearer test-secret-token', got %q", auth)
	}
}

func TestClient_HTTP_Reconnect(t *testing.T) {
	ts := newEchoHTTPServer(t)
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

	// First call should succeed.
	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "first"})
	if err != nil {
		t.Fatalf("first CallTool failed: %v", err)
	}
	if result.Content[0].Text != "echo: first" {
		t.Errorf("expected 'echo: first', got %q", result.Content[0].Text)
	}

	// Simulate session loss by clearing the internal session.
	client.clearSession()

	// Next call should reconnect and succeed.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	result, err = client.CallTool(ctx2, "echo", map[string]any{"msg": "second"})
	if err != nil {
		t.Fatalf("reconnect CallTool failed: %v", err)
	}
	if result.Content[0].Text != "echo: second" {
		t.Errorf("expected 'echo: second', got %q", result.Content[0].Text)
	}
}

func TestClient_HTTP_Timeout(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	// With a generous timeout, the call should succeed.
	ctx := context.Background()
	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "timed"})
	if err != nil {
		t.Fatalf("CallTool with timeout failed: %v", err)
	}
	if result.Content[0].Text != "echo: timed" {
		t.Errorf("expected 'echo: timed', got %q", result.Content[0].Text)
	}
}

// -- gRPC Transport Tests --

// newGRPCEchoServer creates a gRPC MCP server with an echo tool on a random
// TCP port and returns the listener address (host:port) and a cleanup function.
func newGRPCEchoServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	mcp.AddTool(mcpServer,
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

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	handler := emcpgrpc.NewGRPCHandler(func() *mcp.Server { return mcpServer })
	emcpv1.RegisterMCPTransportServer(grpcServer, handler)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	return lis.Addr().String(), func() {
		grpcServer.Stop()
		_ = lis.Close()
	}
}

func TestClient_GRPC_ListTools(t *testing.T) {
	addr, cleanup := newGRPCEchoServer(t)
	defer cleanup()

	client, err := NewClient(Config{
		Endpoint:  addr,
		Transport: TransportGRPC,
	})
	if err != nil {
		t.Fatalf("NewClient gRPC failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", tools[0].Name)
	}
}

func TestClient_GRPC_CallTool(t *testing.T) {
	addr, cleanup := newGRPCEchoServer(t)
	defer cleanup()

	client, err := NewClient(Config{
		Endpoint:  addr,
		Transport: TransportGRPC,
	})
	if err != nil {
		t.Fatalf("NewClient gRPC failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "via grpc"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "echo: via grpc" {
		t.Errorf("expected 'echo: via grpc', got %q", result.Content[0].Text)
	}
}

func TestClient_GRPC_Ping(t *testing.T) {
	addr, cleanup := newGRPCEchoServer(t)
	defer cleanup()

	client, err := NewClient(Config{
		Endpoint:  addr,
		Transport: TransportGRPC,
	})
	if err != nil {
		t.Fatalf("NewClient gRPC failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// -- PID File Tests --

func TestClient_PIDFile(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	// Extract port from test server URL.
	// httptest URL format: "http://127.0.0.1:PORT"
	parts := strings.Split(ts.URL, ":")
	port := parts[len(parts)-1]

	// Write a PID file.
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "test.pid")
	pidData := map[string]any{
		"pid":  os.Getpid(),
		"port": mustAtoi(t, port),
		"name": "test-daemon",
	}
	data, err := json.Marshal(pidData)
	if err != nil {
		t.Fatalf("marshal pid data: %v", err)
	}
	if err := os.WriteFile(pidPath, data, 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	client, err := NewClient(Config{
		PIDFile:   pidPath,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient with PIDFile failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools via PIDFile failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", tools[0].Name)
	}
}

func TestClient_PIDFile_WithToken(t *testing.T) {
	// Track auth header.
	var gotAuth string
	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	mcp.AddTool(mcpServer,
		&mcp.Tool{Name: "noop", Description: "noop"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
			}, nil, nil
		},
	)
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return mcpServer },
		nil,
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		handler.ServeHTTP(w, r)
	}))
	defer ts.Close()

	parts := strings.Split(ts.URL, ":")
	port := parts[len(parts)-1]

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "test.pid")
	pidData := map[string]any{
		"pid":   os.Getpid(),
		"port":  mustAtoi(t, port),
		"name":  "test-daemon",
		"token": "pid-file-token",
	}
	data, err := json.Marshal(pidData)
	if err != nil {
		t.Fatalf("marshal pid data: %v", err)
	}
	if err := os.WriteFile(pidPath, data, 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	client, err := NewClient(Config{
		PIDFile:   pidPath,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	if gotAuth != "Bearer pid-file-token" {
		t.Errorf("expected 'Bearer pid-file-token', got %q", gotAuth)
	}
}

func TestClient_PIDFile_NotFound(t *testing.T) {
	_, err := NewClient(Config{
		PIDFile:   filepath.Join(t.TempDir(), "nonexistent.pid"),
		Transport: TransportHTTP,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent PID file, got nil")
	}
}

// -- Config validation tests --

func TestClient_NoEndpoint(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
}

func TestClient_StdioNotImplemented(t *testing.T) {
	_, err := NewClient(Config{
		Command:   "/usr/bin/echo",
		Transport: TransportStdio,
	})
	if err == nil {
		t.Fatal("expected error for stdio transport, got nil")
	}
}

// -- Discovery unit tests --

func TestReadPIDFile_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	data := `{"pid":1234,"port":8094,"name":"gode","token":"secret123"}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}

	if info.PID != 1234 {
		t.Errorf("PID: expected 1234, got %d", info.PID)
	}
	if info.Port != 8094 {
		t.Errorf("Port: expected 8094, got %d", info.Port)
	}
	if info.Name != "gode" {
		t.Errorf("Name: expected 'gode', got %q", info.Name)
	}
	if info.Token != "secret123" {
		t.Errorf("Token: expected 'secret123', got %q", info.Token)
	}
}

func TestReadPIDFile_PlainPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := os.WriteFile(path, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}

	if info.PID != 42 {
		t.Errorf("PID: expected 42, got %d", info.PID)
	}
	if info.Port != 0 {
		t.Errorf("Port: expected 0, got %d", info.Port)
	}
}

func TestReadPIDFile_NotFound(t *testing.T) {
	_, err := ReadPIDFile(filepath.Join(t.TempDir(), "nope.pid"))
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadPIDFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.pid")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadPIDFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestReadPIDFile_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadPIDFile(path)
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}

// -- Helpers --

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("failed to parse port %q: %v", s, err)
	}
	return n
}
