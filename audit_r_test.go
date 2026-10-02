// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Review regressions for branch fix/audit-t1-t8 (2026-10-02). R* tests FAIL
// on c999260; they must PASS before the branch is mergeable. Helpers
// (newWireCounter, auditServer) come from audit_test.go.
package emcp_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	emcp "github.com/grpmsoft/emcp-go"
	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

func slowServer(stateful bool) *emcp.Server {
	srv := emcp.NewServer(emcp.ServerConfig{Name: "r", Version: "0", Stateful: stateful})
	srv.AddTool("echo", "", nil, func(_ context.Context, _ string, a map[string]any) (*emcp.ToolResult, error) {
		m, _ := a["msg"].(string)
		return emcp.TextResult("echo: " + m), nil
	})
	srv.AddTool("slow", "", nil, func(ctx context.Context, _ string, _ map[string]any) (*emcp.ToolResult, error) {
		select {
		case <-time.After(600 * time.Millisecond):
		case <-ctx.Done():
		}
		return emcp.TextResult("slow done"), nil
	})
	return srv
}

// R4: http.Client.Timeout applies to the whole request including the
// long-lived legacy GET (SSE) stream. Count GETs on a stateful server with a
// short Timeout.
func TestR4_HTTPClientTimeoutVsLegacyGET(t *testing.T) {
	srv := slowServer(true)
	var gets atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
		srv.HTTPHandler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })
	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP, Timeout: 400 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	time.Sleep(2 * time.Second)
	_, err = client.CallTool(context.Background(), "echo", map[string]any{"msg": "x"})
	t.Logf("after 2s: GET count=%d, CallTool err=%v", gets.Load(), err)
	if err != nil {
		t.Errorf("client with Timeout against a legacy (stateful) server breaks once Timeout elapses: %v", err)
	}
}

// R5: gRPC server dies and comes back on the same address. Is the dead
// stream's error classified as transport, and does the client recover?
func TestR5_GRPCServerRestart(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	gs := grpc.NewServer()
	emcpv1.RegisterMCPTransportServer(gs, slowServer(false).GRPCHandler())
	go func() { _ = gs.Serve(ln) }()

	client, err := emcp.NewClient(emcp.Config{Endpoint: addr, Transport: emcp.TransportGRPC})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.CallTool(context.Background(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	gs.Stop()
	time.Sleep(100 * time.Millisecond)
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	gs2 := grpc.NewServer()
	emcpv1.RegisterMCPTransportServer(gs2, slowServer(false).GRPCHandler())
	go func() { _ = gs2.Serve(ln2) }()
	t.Cleanup(gs2.Stop)

	var errs []error
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := client.CallTool(ctx, "echo", nil)
		cancel()
		errs = append(errs, err)
		if err == nil {
			t.Logf("recovered on call %d after restart", i+1)
			if i > 0 {
				t.Errorf("first call after restart failed (no retry-once): %v", errs[0])
			}
			return
		}
	}
	t.Errorf("client never recovered after gRPC server restart: %v", errs)
}

// R5b: stateful HTTP restart (daemon with Stateful=true): first call after
// restart.
func TestR5b_StatefulHTTPRestart(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ts := httptest.NewUnstartedServer(slowServer(true).HTTPHandler())
	ts.Listener = ln
	ts.Start()
	client, err := emcp.NewClient(emcp.Config{Endpoint: "http://" + addr, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ts.CloseClientConnections()
	ts.Close()
	ln2, _ := net.Listen("tcp", addr)
	ts2 := httptest.NewUnstartedServer(slowServer(true).HTTPHandler())
	ts2.Listener = ln2
	ts2.Start()
	t.Cleanup(func() { ts2.CloseClientConnections(); ts2.Close() })
	_, err = client.CallTool(context.Background(), "echo", nil)
	t.Logf("first call after stateful restart: %v", err)
	if err != nil {
		t.Errorf("no retry-once on ErrSessionMissing: %v", err)
	}
}

// R3: Ping on the stdio transport must not spawn a new server process.
func TestR3_PingOnStdioSpawnsProcesses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EMCP_R3_PIDDIR", dir)
	client, err := emcp.NewClient(emcp.Config{
		Transport: emcp.TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestR3_StdioHelper$"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	count := func() int {
		m, _ := filepath.Glob(filepath.Join(dir, "*.pid"))
		return len(m)
	}
	time.Sleep(100 * time.Millisecond)
	before := count()
	for i := 0; i < 3; i++ {
		if err := client.Ping(context.Background()); err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	after := count()
	t.Logf("server processes spawned: before=%d after 3 pings=%d", before, after)
	if after != before {
		t.Errorf("Ping spawned %d extra stdio server processes", after-before)
	}
}

func TestR3_StdioHelper(t *testing.T) {
	dir := os.Getenv("EMCP_R3_PIDDIR")
	if dir == "" {
		t.Skip("helper only")
	}
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.pid", os.Getpid())), nil, 0o600)
	s := mcp.NewServer(&mcp.Implementation{Name: "h", Version: "0"}, nil)
	s.AddTool(&mcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
		})
	_ = s.Run(context.Background(), &mcp.StdioTransport{})
	os.Exit(0)
}

// R7: server direction of T5 — emcp tool returning resource_link / structured
// content as seen by an SDK client.
func TestR7_ServerSideContentFidelity(t *testing.T) {
	srv := emcp.NewServer(emcp.ServerConfig{})
	srv.AddTool("rich", "", nil, func(context.Context, string, map[string]any) (*emcp.ToolResult, error) {
		return &emcp.ToolResult{
			Content: []emcp.ContentItem{
				{Type: "text", Text: "hello"},
				{Type: "resource_link", URI: "file:///etc/hosts", Name: "hosts", MIMEType: "text/plain"},
				{Type: "resource", Resource: &emcp.ResourceContents{URI: "mem://x", MIMEType: "text/plain", Text: "embedded"}},
			},
			StructuredContent: map[string]any{"answer": 42},
		}, nil
	})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	c := mcp.NewClient(&mcp.Implementation{Name: "sdk", Version: "0"}, nil)
	sess, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "rich", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	for i, ct := range res.Content {
		t.Logf("content[%d]: %T %+v", i, ct, ct)
	}
	t.Logf("structuredContent: %v", res.StructuredContent)
	if _, ok := res.Content[1].(*mcp.ResourceLink); !ok {
		t.Errorf("content[1] is %T, want *mcp.ResourceLink", res.Content[1])
	}
	if _, ok := res.Content[2].(*mcp.EmbeddedResource); !ok {
		t.Errorf("content[2] is %T, want *mcp.EmbeddedResource", res.Content[2])
	}
	if res.StructuredContent == nil {
		t.Error("StructuredContent dropped on the server side")
	}
}

// R1b: a per-call deadline on a slow tool must not invalidate the shared
// session (isTransportError treats context errors as transport failures).
func TestR1b_DeadlineInvalidatesSession(t *testing.T) {
	for _, stateful := range []bool{false, true} {
		t.Run(map[bool]string{false: "stateless", true: "stateful"}[stateful], func(t *testing.T) {
			srv := slowServer(stateful)
			wc := newWireCounter(srv.HTTPHandler())
			ts := httptest.NewServer(wc)
			t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })
			client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			var wg sync.WaitGroup
			var fails, oks atomic.Int64
			var msg atomic.Value
			stop := make(chan struct{})
			for g := 0; g < 8; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-stop:
							return
						default:
						}
						if _, err := client.CallTool(context.Background(), "echo", map[string]any{"msg": "x"}); err != nil {
							fails.Add(1)
							msg.Store(err.Error())
						} else {
							oks.Add(1)
						}
					}
				}()
			}
			for i := 0; i < 5; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				_, _ = client.CallTool(ctx, "slow", nil)
				cancel()
				time.Sleep(50 * time.Millisecond)
			}
			close(stop)
			wg.Wait()
			t.Logf("5 deadlines: initialize=%d server/discover=%d DELETE=%d; concurrent echo ok=%d failed=%d (%v)",
				wc.count("initialize"), wc.count("server/discover"), wc.deletes, oks.Load(), fails.Load(), msg.Load())
			if fails.Load() > 0 {
				t.Errorf("concurrent callers failed because a deadline on another call closed the shared session")
			}
			if wc.count("initialize") > 1 || wc.count("server/discover") > 1 {
				t.Errorf("a per-call deadline must not invalidate the session (initialize=%d discover=%d)", wc.count("initialize"), wc.count("server/discover"))
			}
		})
	}
}

// R2: invalidateSession calls ClientSession.Close() (which sends DELETE on a
// legacy session) while holding c.mu. A server whose DELETE path hangs blocks
// every other caller of the client, regardless of their ctx.
func TestR2_InvalidateUnderLockDeadlock(t *testing.T) {
	srv := slowServer(true)
	var calls atomic.Int64
	h := srv.HTTPHandler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			<-r.Context().Done() // DELETE never completes (hung shutdown path)
			return
		case r.Method == http.MethodPost && r.Header.Get("Mcp-Method") == "tools/call":
			calls.Add(1)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })

	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ts.CloseClientConnections() // kills the legacy GET stream; MaxRetries=-1 => session fails
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	go func() {
		_, err := client.CallTool(context.Background(), "echo", nil)
		t.Logf("first caller returned after %v: %v", time.Since(start).Round(time.Millisecond), err)
	}() // hits 404 -> invalidate -> DELETE hangs
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.CallTool(ctx, "echo", nil); done <- err }()
	select {
	case err := <-done:
		t.Logf("second caller returned after %v: %v", time.Since(start).Round(time.Millisecond), err)
	case <-time.After(3 * time.Second):
		t.Fatal("second caller (ctx=1s) blocked for 3s: invalidateSession holds c.mu while Close() sends DELETE to a hung server")
	}
}
