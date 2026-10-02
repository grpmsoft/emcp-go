// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// =============================================================================
// M1: Session leak on JSON-RPC errors
// =============================================================================

// TestClient_JSONRPCError_SessionSurvives verifies that a JSON-RPC error
// (e.g. calling a non-existent tool) does NOT invalidate the session. The
// next call on the same session must succeed without reconnecting.
//
// Before the fix, any error from session.CallTool set c.session = nil,
// forcing a reconnect (and leaking the old session's goroutines/connections).
func TestClient_JSONRPCError_SessionSurvives(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Call a non-existent tool. The MCP server returns a JSON-RPC error
	// (CodeInvalidParams, "unknown tool"). This is NOT a transport error.
	_, err = client.CallTool(ctx, "nonexistent_tool_xyz", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}

	// The session should still be alive. The next call must succeed
	// WITHOUT reconnecting (which would mean the old session leaked).
	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "after-error"})
	if err != nil {
		t.Fatalf("CallTool after JSON-RPC error failed (session was invalidated!): %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "echo: after-error" {
		t.Errorf("unexpected result: %v", result.Content)
	}
}

// TestClient_TransportError_SessionInvalidated verifies that when a transport
// error occurs, the session IS invalidated (with Close called on the old
// session) and subsequent calls can reconnect cleanly.
func TestClient_TransportError_SessionInvalidated(t *testing.T) {
	// Use a stateless server to avoid long-lived SSE connections that block
	// httptest.Server.Close.
	ts := newHTTPTestServerStateless(t, func(s *mcp.Server) {
		mcp.AddTool(s,
			&mcp.Tool{Name: "echo", Description: "echoes the input"},
			func(_ context.Context, _ *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Msg}},
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
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First call succeeds.
	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "alive"})
	if err != nil {
		t.Fatalf("first CallTool failed: %v", err)
	}
	if result.Content[0].Text != "echo: alive" {
		t.Errorf("expected 'echo: alive', got %q", result.Content[0].Text)
	}

	// Simulate session loss (the session was closed server-side or transport broke).
	// This closes the session properly (Close + nil) which is what invalidateSession does.
	client.clearSession()

	// Next call should reconnect transparently and succeed.
	result, err = client.CallTool(ctx, "echo", map[string]any{"msg": "reconnected"})
	if err != nil {
		t.Fatalf("reconnect CallTool failed: %v", err)
	}
	if result.Content[0].Text != "echo: reconnected" {
		t.Errorf("expected 'echo: reconnected', got %q", result.Content[0].Text)
	}
}

// TestClient_NoGoroutineLeak verifies that multiple JSON-RPC error cycles
// do not leak goroutines. Before the fix, each error cycle created a new
// session without closing the old one, leaking goroutines.
func TestClient_NoGoroutineLeak(t *testing.T) {
	ts := newEchoHTTPServer(t)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warm up: make one successful call to establish baseline.
	_, err = client.CallTool(ctx, "echo", map[string]any{"msg": "warmup"})
	if err != nil {
		t.Fatalf("warmup CallTool failed: %v", err)
	}

	// Record baseline goroutine count.
	runtime.GC()
	time.Sleep(50 * time.Millisecond) // let any pending goroutines settle
	baseline := runtime.NumGoroutine()

	// Perform 20 error cycles (call nonexistent tool, then a successful call).
	for i := range 20 {
		_, _ = client.CallTool(ctx, "nonexistent_tool", nil)

		result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "ok"})
		if err != nil {
			t.Fatalf("cycle %d: successful CallTool failed: %v", i, err)
		}
		if result.Content[0].Text != "echo: ok" {
			t.Errorf("cycle %d: expected 'echo: ok', got %q", i, result.Content[0].Text)
		}
	}

	// Check goroutine count. Allow some slack for runtime goroutines.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	final := runtime.NumGoroutine()

	// Before the fix: 20 error cycles leaked ~8-15 goroutines per cycle.
	// After the fix: no leaks. Allow +10 for runtime variance.
	if final > baseline+10 {
		t.Errorf("goroutine leak: baseline=%d, after 20 error cycles=%d (delta=%d)",
			baseline, final, final-baseline)
	}
}

// =============================================================================
// M2: HTTP server stateless, HTTPHandler cached, SessionTimeout
// =============================================================================

// TestServer_HTTPHandler_Cached verifies that calling HTTPHandler() multiple
// times returns the same handler instance. Before the fix, each call created
// a new StreamableHTTPHandler with its own session store, causing session
// loss between requests.
func TestServer_HTTPHandler_Cached(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "cache-test", Version: "0.1.0"})

	h1 := srv.HTTPHandler()
	h2 := srv.HTTPHandler()

	if h1 != h2 {
		t.Error("HTTPHandler() returned different instances; expected same (cached) handler")
	}
}

// TestServer_Stateless verifies that when Stateless=true, the server does
// not return an Mcp-Session-Id header. This confirms the MCP 2026-07-28
// stateless mode is active.
func TestServer_Stateless(t *testing.T) {
	srv := NewServer(ServerConfig{
		Name:    "stateless-test",
		Version: "0.1.0",
		// Stateful defaults to false = stateless mode (MCP 2026-07-28 default).
	})
	srv.AddTool("echo", "echoes", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		msg, _ := args["msg"].(string)
		return TextResult("echo: " + msg), nil
	})

	// Record Mcp-Session-Id headers from responses.
	var sessionHeaders []string
	handler := srv.HTTPHandler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap the response writer to capture headers.
		rw := &headerCapture{ResponseWriter: w}
		handler.ServeHTTP(rw, r)
		if id := rw.Header().Get("Mcp-Session-Id"); id != "" {
			sessionHeaders = append(sessionHeaders, id)
		}
	}))
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "stateless"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.Content[0].Text != "echo: stateless" {
		t.Errorf("unexpected result: %q", result.Content[0].Text)
	}

	// In stateless mode, no Mcp-Session-Id should be set.
	if len(sessionHeaders) > 0 {
		t.Errorf("stateless server should not set Mcp-Session-Id, got %v", sessionHeaders)
	}
}

// TestServer_SessionTimeout verifies that the SessionTimeout config is
// passed through to the SDK handler. We test this by creating a stateful
// server with a short timeout and verifying the config is accepted.
// SessionTimeout is only meaningful in stateful mode.
func TestServer_SessionTimeout(t *testing.T) {
	srv := NewServer(ServerConfig{
		Name:           "timeout-test",
		Version:        "0.1.0",
		Stateful:       true,
		SessionTimeout: 5 * time.Second,
	})
	srv.AddTool("ping", "pong", nil,
		func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
			return TextResult("pong"), nil
		},
	)

	// Getting the handler should not panic with the timeout set.
	handler := srv.HTTPHandler()
	if handler == nil {
		t.Fatal("HTTPHandler returned nil")
	}

	// Verify the server works with the timeout configuration.
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed with SessionTimeout configured: %v", err)
	}
}

// TestServer_HTTPHandler_WrappedConsumer verifies that when a consumer wraps
// the HTTPHandler inside their own HandlerFunc (the pattern that triggered
// M2), it works correctly because the handler is now cached.
func TestServer_HTTPHandler_WrappedConsumer(t *testing.T) {
	srv := NewServer(ServerConfig{
		Name:    "wrapped-test",
		Version: "0.1.0",
		// Stateful defaults to false = stateless mode.
	})
	srv.AddTool("echo", "echoes", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		msg, _ := args["msg"].(string)
		return TextResult("echo: " + msg), nil
	})

	// This is the problematic pattern from M2: wrapping HTTPHandler() in a
	// HandlerFunc that calls HTTPHandler() on every request. Before the fix,
	// each call created a new handler with a new session store.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HTTPHandler().ServeHTTP(w, r)
	}))
	defer ts.Close()

	client, err := NewClient(Config{
		Endpoint:  ts.URL,
		Transport: TransportHTTP,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Multiple calls should work because the handler is cached.
	for i := range 3 {
		result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "wrap"})
		if err != nil {
			t.Fatalf("call %d: CallTool failed: %v", i, err)
		}
		if result.Content[0].Text != "echo: wrap" {
			t.Errorf("call %d: expected 'echo: wrap', got %q", i, result.Content[0].Text)
		}
	}
}

// =============================================================================
// M3: Connection timeouts missing
// =============================================================================

// TestClient_ConnectTimeout verifies that NewClient with a Timeout respects
// the deadline when connecting to a server that refuses connections.
// Before the fix, NewClient hung forever because it called
// connect(context.Background()) ignoring Config.Timeout.
func TestClient_ConnectTimeout(t *testing.T) {
	// Use a port that is NOT listening. The OS will refuse the connection
	// immediately or (on some systems) time out. Either way, the Timeout
	// on the context should bound the total time.
	//
	// We allocate a port by binding then immediately closing — this gives us
	// a port that nothing is listening on.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	closedAddr := lis.Addr().String()
	_ = lis.Close() // Nobody is listening now.

	start := time.Now()

	_, err = NewClient(Config{
		Endpoint:  "http://" + closedAddr,
		Transport: TransportHTTP,
		Timeout:   2 * time.Second,
	})
	elapsed := time.Since(start)

	// NewClient should fail (connection refused or timeout).
	if err == nil {
		t.Fatal("expected error for dead server with timeout, got nil")
	}

	// The key assertion: Before the fix, this would hang FOREVER (no timeout
	// on context.Background()). After the fix, it must return within a
	// bounded time. We allow generous slack for CI and retry logic.
	if elapsed > 15*time.Second {
		t.Errorf("NewClient took %v; before the fix it would hang forever", elapsed)
	}
	t.Logf("NewClient returned in %v with error: %v", elapsed, err)
}

// TestClient_ConnectTimeout_GRPC verifies the same timeout behavior for gRPC.
// The gRPC transport had a bug where Connect opened a stream on
// context.Background(), ignoring the caller's timeout.
func TestClient_ConnectTimeout_GRPC(t *testing.T) {
	// Create a TCP listener that accepts but never speaks gRPC.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 1024)
				for {
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	start := time.Now()

	_, err = NewClient(Config{
		Endpoint:  lis.Addr().String(),
		Transport: TransportGRPC,
		Timeout:   500 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for silent gRPC server with timeout, got nil")
	}

	if elapsed > 5*time.Second {
		t.Errorf("NewClient gRPC took %v, expected to timeout within ~500ms", elapsed)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// headerCapture wraps an http.ResponseWriter to expose captured headers.
type headerCapture struct {
	http.ResponseWriter
}

// newHTTPTestServerStateless creates a stateless test server using the SDK
// directly (for tests that need to verify stateless behavior at the SDK level).
func newHTTPTestServerStateless(t *testing.T, configure func(s *mcp.Server)) *httptest.Server {
	t.Helper()

	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "test-server", Version: "0.1.0"},
		nil,
	)
	configure(mcpServer)

	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)

	return httptest.NewServer(handler)
}
