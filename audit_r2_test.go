// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Review round 2 for branch fix/audit-t1-t8 @ 34d8c3a (r2.1: cross-platform kill). R6/R6b are guards
// (must stay green once the retry set is narrowed); R8 FAILS on 34d8c3a.
// TestR3_StdioHelper comes from audit_r_test.go.
package emcp_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	emcp "github.com/grpmsoft/emcp-go"
	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"google.golang.org/grpc"
)

// R6: retry-once must never re-execute a tools/call whose request reached the
// server. Here the server executes the tool, then drops the TCP connection
// before answering (crash after side effect).
func TestR6_RetryDoubleExecutesTool(t *testing.T) {
	var executed atomic.Int64
	srv := emcp.NewServer(emcp.ServerConfig{})
	srv.AddTool("apply_patch", "non-idempotent", nil, func(context.Context, string, map[string]any) (*emcp.ToolResult, error) {
		executed.Add(1)
		return emcp.TextResult("applied"), nil
	})
	h := srv.HTTPHandler()
	var dropped atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("Mcp-Method") == "tools/call" && dropped.CompareAndSwap(false, true) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r) // tool runs, side effect happens
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close() // response never delivered: client sees EOF/reset
			}
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })

	client, err := emcp.NewClient(emcp.Config{Endpoint: ts.URL, Transport: emcp.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	res, err := client.CallTool(context.Background(), "apply_patch", nil)
	t.Logf("CallTool: err=%v res=%+v executed=%d", err, res, executed.Load())
	if n := executed.Load(); n != 1 {
		t.Errorf("tool executed %d times for one CallTool (retry after mid-flight transport error re-ran a non-idempotent tool)", n)
	}
}

// R6b: same as R6 over gRPC — server goes down after the tool ran but before
// the response was delivered, then comes back on the same address.
func TestR6b_GRPCRetryDoubleExecutesTool(t *testing.T) {
	var executed atomic.Int64
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	gs := grpc.NewServer()
	var gsStop func()
	mk := func() *emcp.Server {
		srv := emcp.NewServer(emcp.ServerConfig{})
		srv.AddTool("apply_patch", "non-idempotent", nil, func(context.Context, string, map[string]any) (*emcp.ToolResult, error) {
			if executed.Add(1) == 1 {
				go gsStop() // crash after the side effect, before the reply
				time.Sleep(300 * time.Millisecond)
			}
			return emcp.TextResult("applied"), nil
		})
		return srv
	}
	emcpv1.RegisterMCPTransportServer(gs, mk().GRPCHandler())
	go func() { _ = gs.Serve(ln) }()
	restarted := make(chan struct{})
	gsStop = func() {
		gs.Stop()
		ln2, err := net.Listen("tcp", addr)
		if err != nil {
			t.Logf("relisten: %v", err)
			return
		}
		gs2 := grpc.NewServer()
		emcpv1.RegisterMCPTransportServer(gs2, mk().GRPCHandler())
		go func() { _ = gs2.Serve(ln2) }()
		t.Cleanup(gs2.Stop)
		close(restarted)
	}

	client, err := emcp.NewClient(emcp.Config{Endpoint: addr, Transport: emcp.TransportGRPC})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := client.CallTool(ctx, "apply_patch", nil)
	<-restarted
	t.Logf("CallTool: err=%v res=%+v executed=%d", err, res, executed.Load())
	if n := executed.Load(); n != 1 {
		t.Errorf("tool executed %d times for one CallTool (retry re-ran a non-idempotent tool after a mid-flight stream loss)", n)
	}
}

// R8: Ping on stdio returns nil even after the child process is dead.
func TestR8_StdioPingDeadChild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EMCP_R3_PIDDIR", dir)
	client, err := emcp.NewClient(emcp.Config{Transport: emcp.TransportStdio, Command: os.Args[0], Args: []string{"-test.run=^TestR3_StdioHelper$"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	time.Sleep(100 * time.Millisecond)
	m, _ := filepath.Glob(filepath.Join(dir, "*.pid"))
	if len(m) != 1 {
		t.Fatalf("pid files: %v", m)
	}
	pid, _ := strconv.Atoi(strings.TrimSuffix(filepath.Base(m[0]), ".pid"))
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil { // SIGKILL on Unix, TerminateProcess on Windows
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	err = client.Ping(context.Background())
	t.Logf("Ping after SIGKILL of the stdio server: %v", err)
	if err == nil {
		t.Error("Ping reported a dead stdio server as alive")
	}
	_, err = client.CallTool(context.Background(), "echo", nil)
	t.Logf("CallTool after SIGKILL: %v", err)
}
