package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/goco-ai/emcp-go/toolmeta"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stubHandler returns a MethodHandler that returns a fixed result.
func stubHandler(result mcp.Result, err error) mcp.MethodHandler {
	return func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return result, err
	}
}

// panicHandler returns a MethodHandler that panics with the given value.
func panicHandler(v any) mcp.MethodHandler {
	return func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		panic(v)
	}
}

// slowHandler returns a MethodHandler that blocks for the given duration.
func slowHandler(d time.Duration) mcp.MethodHandler {
	return func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		select {
		case <-time.After(d):
			return &mcp.CallToolResult{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// makeToolCallRequest creates a *mcp.CallToolRequest with the given tool name.
// This is the typed request that the SDK passes to middleware for "tools/call".
func makeToolCallRequest(toolName string) mcp.Request {
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: toolName,
		},
	}
}

// discardLogger returns a logger that writes nowhere.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---------------------------------------------------------------------------
// Recovery tests
// ---------------------------------------------------------------------------

func TestRecovery_CatchesPanic(t *testing.T) {
	tests := []struct {
		name       string
		panicValue any
	}{
		{"string panic", "something went wrong"},
		{"error panic", errors.New("boom")},
		{"integer panic", 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := Recovery(discardLogger())
			handler := mw(panicHandler(tt.panicValue))

			result, err := handler(context.Background(), "tools/call", nil)
			if result != nil {
				t.Errorf("expected nil result, got %v", result)
			}
			if err == nil {
				t.Fatal("expected error after panic, got nil")
			}

			var rpcErr *jsonrpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("expected *jsonrpc.Error, got %T: %v", err, err)
			}
			if rpcErr.Code != jsonrpc.CodeInternalError {
				t.Errorf("error code = %d, want %d", rpcErr.Code, jsonrpc.CodeInternalError)
			}
		})
	}
}

func TestRecovery_PassesThroughNormal(t *testing.T) {
	expected := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
	}
	mw := Recovery(discardLogger())
	handler := mw(stubHandler(expected, nil))

	result, err := handler(context.Background(), "tools/call", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctr, ok := result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected *CallToolResult, got %T", result)
	}
	text := ctr.Content[0].(*mcp.TextContent).Text
	if text != "ok" {
		t.Errorf("content = %q, want %q", text, "ok")
	}
}

func TestRecovery_PassesThroughError(t *testing.T) {
	handlerErr := errors.New("handler failed")
	mw := Recovery(discardLogger())
	handler := mw(stubHandler(nil, handlerErr))

	_, err := handler(context.Background(), "tools/call", nil)
	if !errors.Is(err, handlerErr) {
		t.Errorf("err = %v, want %v", err, handlerErr)
	}
}

func TestRecovery_NilLogger(t *testing.T) {
	// Must not panic when logger is nil.
	mw := Recovery(nil)
	handler := mw(panicHandler("boom"))

	_, err := handler(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error after panic, got nil")
	}
}

func TestRecovery_LogsDetails(t *testing.T) {
	var buf logBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	mw := Recovery(logger)
	handler := mw(panicHandler("kaboom"))

	_, _ = handler(context.Background(), "tools/call", nil)

	logged := buf.String()
	if logged == "" {
		t.Fatal("expected log output, got empty")
	}
}

// logBuffer is a thread-safe bytes buffer for capturing log output.
type logBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.mu.Unlock()
	return len(p), nil
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// ---------------------------------------------------------------------------
// Metrics tests
// ---------------------------------------------------------------------------

func TestMetrics_RecordsSuccess(t *testing.T) {
	rec := NewInMemoryRecorder()
	mw := Metrics(rec)

	handler := mw(stubHandler(&mcp.CallToolResult{}, nil))
	_, _ = handler(context.Background(), "tools/call", nil)
	_, _ = handler(context.Background(), "tools/call", nil)

	stats := rec.Stats("tools/call")
	if stats.Calls != 2 {
		t.Errorf("calls = %d, want 2", stats.Calls)
	}
	if stats.Errors != 0 {
		t.Errorf("errors = %d, want 0", stats.Errors)
	}
	if stats.TotalNanos < 0 {
		t.Error("total latency should be non-negative")
	}
}

func TestMetrics_RecordsErrors(t *testing.T) {
	rec := NewInMemoryRecorder()
	mw := Metrics(rec)

	handler := mw(stubHandler(nil, errors.New("fail")))
	_, _ = handler(context.Background(), "tools/call", nil)

	stats := rec.Stats("tools/call")
	if stats.Calls != 1 {
		t.Errorf("calls = %d, want 1", stats.Calls)
	}
	if stats.Errors != 1 {
		t.Errorf("errors = %d, want 1", stats.Errors)
	}
}

func TestMetrics_SeparatesMethods(t *testing.T) {
	rec := NewInMemoryRecorder()
	mw := Metrics(rec)

	handler := mw(stubHandler(&mcp.CallToolResult{}, nil))
	_, _ = handler(context.Background(), "tools/call", nil)
	_, _ = handler(context.Background(), "tools/list", nil)
	_, _ = handler(context.Background(), "tools/call", nil)

	if s := rec.Stats("tools/call"); s.Calls != 2 {
		t.Errorf("tools/call calls = %d, want 2", s.Calls)
	}
	if s := rec.Stats("tools/list"); s.Calls != 1 {
		t.Errorf("tools/list calls = %d, want 1", s.Calls)
	}
}

func TestMetrics_UnobservedMethodReturnsZero(t *testing.T) {
	rec := NewInMemoryRecorder()
	stats := rec.Stats("never/called")
	if stats.Calls != 0 || stats.Errors != 0 || stats.TotalNanos != 0 {
		t.Errorf("expected zero stats for unobserved method, got %+v", stats)
	}
}

func TestMetrics_ConcurrentSafety(t *testing.T) {
	rec := NewInMemoryRecorder()
	mw := Metrics(rec)
	handler := mw(stubHandler(&mcp.CallToolResult{}, nil))

	const goroutines = 50
	const callsPerGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range callsPerGoroutine {
				_, _ = handler(context.Background(), "tools/call", nil)
			}
		}()
	}
	wg.Wait()

	stats := rec.Stats("tools/call")
	expected := int64(goroutines * callsPerGoroutine)
	if stats.Calls != expected {
		t.Errorf("calls = %d, want %d", stats.Calls, expected)
	}
}

// ---------------------------------------------------------------------------
// Risk gate tests
// ---------------------------------------------------------------------------

func TestRiskGate_BlocksHighRisk(t *testing.T) {
	tests := []struct {
		name      string
		maxLevel  toolmeta.RiskLevel
		toolLevel toolmeta.RiskLevel
		wantBlock bool
	}{
		{"low max blocks medium", toolmeta.RiskLow, toolmeta.RiskMedium, true},
		{"low max blocks high", toolmeta.RiskLow, toolmeta.RiskHigh, true},
		{"low max blocks critical", toolmeta.RiskLow, toolmeta.RiskCritical, true},
		{"medium max allows low", toolmeta.RiskMedium, toolmeta.RiskLow, false},
		{"medium max allows medium", toolmeta.RiskMedium, toolmeta.RiskMedium, false},
		{"medium max blocks high", toolmeta.RiskMedium, toolmeta.RiskHigh, true},
		{"high max allows high", toolmeta.RiskHigh, toolmeta.RiskHigh, false},
		{"high max blocks critical", toolmeta.RiskHigh, toolmeta.RiskCritical, true},
		{"critical max allows critical", toolmeta.RiskCritical, toolmeta.RiskCritical, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &MaxRiskPolicy{MaxLevel: tt.maxLevel}
			toolRisks := map[string]toolmeta.RiskLevel{
				"target_tool": tt.toolLevel,
			}

			mw := RiskGate(policy, toolRisks)
			called := false
			handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				called = true
				return &mcp.CallToolResult{}, nil
			})

			req := makeToolCallRequest("target_tool")
			_, err := handler(context.Background(), MethodToolsCall, req)

			if tt.wantBlock {
				if err == nil {
					t.Fatal("expected block error, got nil")
				}
				var rpcErr *jsonrpc.Error
				if !errors.As(err, &rpcErr) {
					t.Fatalf("expected *jsonrpc.Error, got %T", err)
				}
				if called {
					t.Error("handler should not have been called")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !called {
					t.Error("handler should have been called")
				}
			}
		})
	}
}

func TestRiskGate_UnknownToolDefaultsLow(t *testing.T) {
	policy := &MaxRiskPolicy{MaxLevel: toolmeta.RiskLow}
	toolRisks := map[string]toolmeta.RiskLevel{} // empty registry

	mw := RiskGate(policy, toolRisks)
	called := false
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	})

	req := makeToolCallRequest("unknown_tool")
	_, err := handler(context.Background(), MethodToolsCall, req)

	if err != nil {
		t.Fatalf("unknown tool with low max should pass, got: %v", err)
	}
	if !called {
		t.Error("handler should have been called")
	}
}

func TestRiskGate_SkipsNonToolMethods(t *testing.T) {
	policy := &MaxRiskPolicy{MaxLevel: toolmeta.RiskLow}
	toolRisks := map[string]toolmeta.RiskLevel{
		"dangerous": toolmeta.RiskCritical,
	}

	mw := RiskGate(policy, toolRisks)
	called := false
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	})

	// Ping, not tools/call — should pass through regardless.
	_, err := handler(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("non-tool method should pass through, got: %v", err)
	}
	if !called {
		t.Error("handler should have been called for non-tool method")
	}
}

// ---------------------------------------------------------------------------
// Timeout tests
// ---------------------------------------------------------------------------

func TestTimeout_CancelsSlow(t *testing.T) {
	toolTimeouts := map[string]time.Duration{
		"slow_tool": 50 * time.Millisecond,
	}

	mw := Timeout(toolTimeouts, 0)
	handler := mw(slowHandler(5 * time.Second))

	req := makeToolCallRequest("slow_tool")
	_, err := handler(context.Background(), MethodToolsCall, req)

	if err == nil {
		t.Fatal("expected context deadline exceeded, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestTimeout_AllowsFast(t *testing.T) {
	toolTimeouts := map[string]time.Duration{
		"fast_tool": 5 * time.Second,
	}

	mw := Timeout(toolTimeouts, 0)
	handler := mw(stubHandler(&mcp.CallToolResult{}, nil))

	req := makeToolCallRequest("fast_tool")
	_, err := handler(context.Background(), MethodToolsCall, req)

	if err != nil {
		t.Fatalf("fast tool should succeed, got: %v", err)
	}
}

func TestTimeout_UsesDefaultForUnmapped(t *testing.T) {
	toolTimeouts := map[string]time.Duration{} // no per-tool entries
	defaultTimeout := 50 * time.Millisecond

	mw := Timeout(toolTimeouts, defaultTimeout)
	handler := mw(slowHandler(5 * time.Second))

	req := makeToolCallRequest("unmapped_tool")
	_, err := handler(context.Background(), MethodToolsCall, req)

	if err == nil {
		t.Fatal("expected timeout for unmapped tool with short default, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestTimeout_ZeroDefaultNoTimeout(t *testing.T) {
	toolTimeouts := map[string]time.Duration{}

	mw := Timeout(toolTimeouts, 0) // zero default = no timeout for unmapped
	handler := mw(stubHandler(&mcp.CallToolResult{}, nil))

	req := makeToolCallRequest("any_tool")
	_, err := handler(context.Background(), MethodToolsCall, req)

	if err != nil {
		t.Fatalf("zero default should not add timeout, got: %v", err)
	}
}

func TestTimeout_SkipsNonToolMethods(t *testing.T) {
	toolTimeouts := map[string]time.Duration{
		"any": 1 * time.Nanosecond,
	}

	mw := Timeout(toolTimeouts, 1*time.Nanosecond)
	// Use a handler that checks context is NOT already cancelled.
	handler := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context should not be cancelled for non-tool methods")
		}
		return &mcp.CallToolResult{}, nil
	})

	_, err := handler(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("non-tool method should not have timeout: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration: middleware composition
// ---------------------------------------------------------------------------

func TestComposition_RecoveryPlusMetrics(t *testing.T) {
	rec := NewInMemoryRecorder()
	logger := discardLogger()

	// Compose: Recovery wraps Metrics wraps handler.
	// Recovery should catch the panic, and Metrics should still record the error.
	inner := panicHandler("boom")
	composed := Recovery(logger)(Metrics(rec)(inner))

	_, err := composed(context.Background(), "tools/call", nil)
	if err == nil {
		t.Fatal("expected error from recovered panic")
	}

	stats := rec.Stats("tools/call")
	// The panic happens inside Metrics' next(), which means Metrics itself panics.
	// Recovery catches it at the outer level. So Metrics does NOT record.
	// This is the expected layering: Recovery(Metrics(handler)).
	// If we want Metrics to record, Recovery must be inner: Metrics(Recovery(handler)).
	// Let's test the correct composition for recording.
	_ = stats
}

func TestComposition_MetricsWrapsRecovery(t *testing.T) {
	rec := NewInMemoryRecorder()
	logger := discardLogger()

	// Metrics(Recovery(handler)) — Recovery catches panic and returns error,
	// Metrics records the error.
	inner := panicHandler("boom")
	composed := Metrics(rec)(Recovery(logger)(inner))

	_, err := composed(context.Background(), "test/panic", nil)
	if err == nil {
		t.Fatal("expected error from recovered panic")
	}

	stats := rec.Stats("test/panic")
	if stats.Calls != 1 {
		t.Errorf("calls = %d, want 1", stats.Calls)
	}
	if stats.Errors != 1 {
		t.Errorf("errors = %d, want 1", stats.Errors)
	}
}

func TestComposition_RiskPlusTimeout(t *testing.T) {
	policy := &MaxRiskPolicy{MaxLevel: toolmeta.RiskMedium}
	toolRisks := map[string]toolmeta.RiskLevel{
		"safe":      toolmeta.RiskLow,
		"dangerous": toolmeta.RiskCritical,
	}
	toolTimeouts := map[string]time.Duration{
		"safe": 5 * time.Second,
	}

	// RiskGate wraps Timeout wraps handler.
	inner := stubHandler(&mcp.CallToolResult{}, nil)
	composed := RiskGate(policy, toolRisks)(Timeout(toolTimeouts, 0)(inner))

	// Safe tool should pass.
	req := makeToolCallRequest("safe")
	_, err := composed(context.Background(), MethodToolsCall, req)
	if err != nil {
		t.Fatalf("safe tool should pass: %v", err)
	}

	// Dangerous tool should be blocked before timeout is even applied.
	req = makeToolCallRequest("dangerous")
	_, err = composed(context.Background(), MethodToolsCall, req)
	if err == nil {
		t.Fatal("dangerous tool should be blocked")
	}
}

// ---------------------------------------------------------------------------
// Integration: full MCP server with NewInMemoryTransports
// ---------------------------------------------------------------------------

func TestIntegration_MiddlewareWithRealServer(t *testing.T) {
	rec := NewInMemoryRecorder()
	logger := discardLogger()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "0.1.0",
	}, nil)

	// Install middleware stack: Recovery -> Metrics -> handler.
	server.AddReceivingMiddleware(
		Recovery(logger),
		Metrics(rec),
	)

	// Add a simple tool.
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "echo",
			Description: "Echo back the input.",
		},
		func(_ context.Context, req *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
			msg, _ := args["message"].(string)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			}, msg, nil
		},
	)

	// Connect via in-memory transports.
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	ct, st := mcp.NewInMemoryTransports()

	ctx := context.Background()

	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	// Call the tool.
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if tc.Text != "hello" {
		t.Errorf("text = %q, want %q", tc.Text, "hello")
	}

	// Verify metrics recorded the tool call.
	// The SDK method name for tool calls is "tools/call".
	stats := rec.Stats("tools/call")
	if stats.Calls < 1 {
		t.Errorf("tools/call calls = %d, want >= 1", stats.Calls)
	}
	if stats.Errors != 0 {
		t.Errorf("tools/call errors = %d, want 0", stats.Errors)
	}
}

func TestIntegration_PanicRecoveryWithRealServer(t *testing.T) {
	logger := discardLogger()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "0.1.0",
	}, nil)

	server.AddReceivingMiddleware(Recovery(logger))

	// Add a tool that panics.
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "crasher",
			Description: "A tool that always panics.",
		},
		func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
			panic("intentional crash")
		},
	)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	ct, st := mcp.NewInMemoryTransports()

	ctx := context.Background()

	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	// Call the panicking tool. Should get an error, not a crash.
	_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "crasher",
	})
	if err == nil {
		t.Fatal("expected error from panicking tool, got nil")
	}

	// The server should still be alive — call ping to verify.
	if pingErr := clientSession.Ping(ctx, nil); pingErr != nil {
		t.Fatalf("server should still be alive after panic recovery: %v", pingErr)
	}
}
