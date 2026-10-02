// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Audit regression tests (2026-10-02). Each test reproduces one finding from
// AUDIT-20261002.md and is expected to FAIL on commit 3ec19f2 and PASS once
// the corresponding task card is done. Test names carry the card ID.
package emcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	emcp "github.com/grpmsoft/emcp-go"
	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ---------------------------------------------------------------- helpers

// wireCounter counts JSON-RPC methods in POST bodies, plus GET/DELETE requests.
type wireCounter struct {
	next    http.Handler
	mu      sync.Mutex
	methods map[string]int
	gets    int
	deletes int
}

func newWireCounter(next http.Handler) *wireCounter {
	return &wireCounter{next: next, methods: map[string]int{}}
}

func (c *wireCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	switch r.Method {
	case http.MethodGet:
		c.gets++
	case http.MethodDelete:
		c.deletes++
	case http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var m struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &m)
		c.methods[m.Method]++
	}
	c.mu.Unlock()
	c.next.ServeHTTP(w, r)
}

func (c *wireCounter) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.methods[method]
}

func (c *wireCounter) getCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets
}

func auditServer() *emcp.Server {
	srv := emcp.NewServer(emcp.ServerConfig{Name: "audit", Version: "0"})
	srv.AddTool("echo", "echo", map[string]any{"type": "object"},
		func(_ context.Context, _ string, args map[string]any) (*emcp.ToolResult, error) {
			msg, _ := args["msg"].(string)
			return emcp.TextResult("echo: " + msg), nil
		})
	srv.AddTool("boom", "handler returns a Go error", map[string]any{"type": "object"},
		func(context.Context, string, map[string]any) (*emcp.ToolResult, error) {
			return nil, errors.New("file not found")
		})
	return srv
}

// hangingListener accepts TCP connections and never answers.
func hangingListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { time.Sleep(time.Minute); _ = c.Close() }()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// ---------------------------------------------------------------- M1

// M1: a JSON-RPC error (handler error, unknown tool) must not invalidate the
// session. Today every such error forces a full reconnect and leaks the old
// session (no Close, no DELETE, hanging GET stream, ~7 goroutines).
func TestAudit_M1_JSONRPCErrorKeepsSession(t *testing.T) {
	srv := auditServer()
	wc := newWireCounter(srv.HTTPHandler())
	ts := httptest.NewServer(wc)
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })

	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := client.CallTool(ctx, "boom", nil); err == nil {
			t.Fatal("boom must return an error")
		}
		if _, err := client.CallTool(ctx, "no-such-tool", nil); err == nil {
			t.Fatal("unknown tool must return an error")
		}
		if _, err := client.CallTool(ctx, "echo", map[string]any{"msg": "ok"}); err != nil {
			t.Fatalf("echo after errors failed: %v", err)
		}
	}

	init, disc, gets := wc.count("initialize"), wc.count("server/discover"), wc.getCount()
	t.Logf("initialize=%d server/discover=%d GET=%d", init, disc, gets)
	// One connect = at most one server/discover (+ one legacy initialize).
	if init > 1 || disc > 1 || gets > 1 {
		t.Errorf("session was re-established after JSON-RPC errors: initialize=%d discover=%d GET=%d (want <=1 each)", init, disc, gets)
	}
}

// M1: after the server restarts on the same address (daemon restart), the
// first call must succeed: dead session detected, closed, reconnected, and the
// request retried once when it could not have been executed (connection
// refused / unknown session). Today the first call fails, the second works.
func TestAudit_M1_TransparentReconnectAfterRestart(t *testing.T) {
	srv := auditServer()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ts := httptest.NewUnstartedServer(srv.HTTPHandler())
	ts.Listener = ln
	ts.Start()

	client, err := emcp.NewClient(emcp.Config{Endpoint: "http://" + addr, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "1"}); err != nil {
		t.Fatal(err)
	}

	// Restart: kill the server, bring a fresh one up on the same address.
	ts.CloseClientConnections()
	ts.Close()
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("re-listen on %s: %v", addr, err)
	}
	ts2 := httptest.NewUnstartedServer(auditServer().HTTPHandler())
	ts2.Listener = ln2
	ts2.Start()
	t.Cleanup(func() { ts2.CloseClientConnections(); ts2.Close() })

	res, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "2"})
	if err != nil {
		t.Fatalf("first call after server restart failed (no transparent reconnect): %v", err)
	}
	if res.Content[0].Text != "echo: 2" {
		t.Fatalf("unexpected result %+v", res)
	}
}

// ---------------------------------------------------------------- M2

// M2: HTTPHandler() must return the same handler every time (it owns the
// session store). GRPCHandler() is already cached; HTTPHandler() is not.
func TestAudit_M2_HTTPHandlerIsCached(t *testing.T) {
	srv := auditServer()
	if srv.HTTPHandler() != srv.HTTPHandler() {
		t.Error("HTTPHandler() returns a new handler (new session store) on every call")
	}

	// The natural per-request wrapper must work.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HTTPHandler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })
	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatalf("NewClient via per-request HTTPHandler(): %v", err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "x"}); err != nil {
		t.Fatalf("CallTool via per-request HTTPHandler(): %v", err)
	}
}

// M2: the default HTTP handler must serve MCP 2026-07-28 (stateless), so a
// 2026-07-28 client does not negotiate down to 2025-11-25 and no
// Mcp-Session-Id is ever issued. Stateful mode stays available via
// ServerConfig (opt-in).
func TestAudit_M2_StatelessByDefault(t *testing.T) {
	srv := auditServer()
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })

	c := mcp.NewClient(&mcp.Implementation{Name: "audit", Version: "0"}, nil)
	sess, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if v := sess.InitializeResult().ProtocolVersion; v != "2026-07-28" {
		t.Errorf("negotiated %s, want 2026-07-28 (handler is stateful: StreamableHTTPOptions == nil)", v)
	}
	if id := sess.ID(); id != "" {
		t.Errorf("server issued Mcp-Session-Id %q; stateless servers must not", id)
	}
}

// M2: "ping" was removed in 2026-07-28. Client.Ping must still work as a
// liveness check against a 2026-07-28 server (e.g. by a server/discover
// round-trip) instead of failing -- and, per M1, must not drop the session.
func TestAudit_M2_PingWorksOnNewProtocol(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "sl", Version: "1"}, nil)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping against a 2026-07-28 (stateless) server failed: %v", err)
	}
}

// ---------------------------------------------------------------- M3

// M3: Config.Timeout must bound the initial connect performed by NewClient.
// Today NewClient connects with context.Background() and hangs forever
// against a server that accepts TCP and never answers.
func TestAudit_M3_NewClientHonorsTimeout(t *testing.T) {
	ln := hangingListener(t)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := emcp.NewClient(emcp.Config{
			Endpoint:  "http://" + ln.Addr().String() + "/mcp",
			Transport: emcp.TransportHTTP,
			Timeout:   300 * time.Millisecond,
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("NewClient took %v with Timeout=300ms", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NewClient(Timeout=300ms) still blocked after 5s against a hanging server")
	}
}

// ---------------------------------------------------------------- M4

// M4: content types beyond text/image/audio must survive conversion. Today
// resource_link and embedded resources become {Type:"text", Text:""} and
// structuredContent is dropped entirely.
func TestAudit_M4_ContentTypesPreserved(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "3p", Version: "1"}, nil)
	s.AddTool(&mcp.Tool{Name: "rich", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "hello"},
					&mcp.ResourceLink{URI: "file:///etc/hosts", Name: "hosts", MIMEType: "text/plain"},
					&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "mem://x", MIMEType: "text/plain", Text: "embedded"}},
				},
				StructuredContent: map[string]any{"answer": 42},
			}, nil
		})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })

	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	res, err := client.CallTool(context.Background(), "rich", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("got %d content items, want 3", len(res.Content))
	}
	if got := res.Content[1].Type; got != "resource_link" {
		t.Errorf("content[1].Type = %q, want resource_link", got)
	}
	if got := res.Content[2].Type; got != "resource" {
		t.Errorf("content[2].Type = %q, want resource", got)
	}
	for i, c := range res.Content[1:] {
		if c.Type == "text" && c.Text == "" {
			t.Errorf("content[%d] silently degraded to empty text", i+1)
		}
	}
	// After the fix, extend: res.StructuredContent must equal {"answer":42}
	// and ContentItem must carry URI/Name for resource_link and the embedded
	// ResourceContents for resource.
}

// ---------------------------------------------------------------- M5

// M5: stdio transport is documented in README/CHANGELOG/ROADMAP/AGENTS/llms.txt
// but NewClient returns "stdio transport not yet implemented". The helper
// process below is an SDK stdio server; the test re-executes the test binary
// as that server (helper-process pattern).
func TestAudit_M5_StdioTransport(t *testing.T) {
	t.Setenv("EMCP_AUDIT_STDIO_HELPER", "1")
	client, err := emcp.NewClient(emcp.Config{
		Transport: emcp.TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestAudit_M5_StdioHelperProcess$"},
	})
	if err != nil {
		t.Fatalf("stdio NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()
	res, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "stdio"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].Text != "echo: stdio" {
		t.Fatalf("unexpected %+v", res)
	}
}

// TestAudit_M5_StdioHelperProcess is not a test: when EMCP_AUDIT_STDIO_HELPER
// is set it serves an MCP server on stdin/stdout until EOF, then exits.
func TestAudit_M5_StdioHelperProcess(t *testing.T) {
	if os.Getenv("EMCP_AUDIT_STDIO_HELPER") != "1" {
		t.Skip("helper process only")
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "stdio-helper", Version: "0"}, nil)
	s.AddTool(&mcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &args)
			msg, _ := args["msg"].(string)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + msg}}}, nil
		})
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// M5: TransportAuto is documented as "gRPC first, then HTTP"; today it is
// plain HTTP. With a host:port endpoint and a gRPC-only server, Auto must
// connect.
func TestAudit_M5_TransportAutoTriesGRPC(t *testing.T) {
	srv := auditServer()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	emcpv1.RegisterMCPTransportServer(gs, srv.GRPCHandler())
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(gs.Stop)

	client, err := emcp.NewClient(emcp.Config{Endpoint: ln.Addr().String(), Transport: emcp.TransportAuto})
	if err != nil {
		t.Fatalf("TransportAuto against a gRPC-only host:port endpoint: %v", err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "auto"}); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- M7

// M7: BearerToken must be sent on the gRPC transport too (metadata
// "authorization: Bearer <token>"). Today it is read from the PID file and
// then silently ignored for gRPC.
func TestAudit_M7_GRPCClientSendsBearer(t *testing.T) {
	srv := auditServer()
	var (
		mu   sync.Mutex
		seen []string
	)
	gs := grpc.NewServer(grpc.StreamInterceptor(func(s any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		mu.Lock()
		seen = append(seen, md.Get("authorization")...)
		mu.Unlock()
		return h(s, ss)
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	emcpv1.RegisterMCPTransportServer(gs, srv.GRPCHandler())
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(gs.Stop)

	client, err := emcp.NewClient(emcp.Config{Endpoint: ln.Addr().String(), Transport: emcp.TransportGRPC, BearerToken: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.CallTool(context.Background(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || !strings.EqualFold(seen[0], "Bearer s3cret") {
		t.Errorf("gRPC stream carried authorization metadata %v, want [Bearer s3cret]", seen)
	}
}

// ---------------------------------------------------------------- minor

// m1: AddTool must not panic on a caller schema; it should either normalize
// (inject "type":"object") or report the problem as an error.
func TestAudit_m1_AddToolDoesNotPanic(t *testing.T) {
	srv := emcp.NewServer(emcp.ServerConfig{})
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AddTool panicked: %v", r)
		}
	}()
	srv.AddTool("x", "x", map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}},
		func(context.Context, string, map[string]any) (*emcp.ToolResult, error) {
			return emcp.TextResult("ok"), nil
		})
}
