// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Audit regression tests (2026-10-02), see AUDIT-20261002.md.
package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

// M3: Connect must honour ctx while dialing / opening the stream. Today the
// stream is opened on context.Background() and a hanging server makes
// Connect block for gRPC's 20s MinConnectTimeout regardless of ctx.
func TestAudit_M3_ConnectHonorsCtx(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { time.Sleep(time.Minute); _ = c.Close() }()
		}
	}()

	tr := &GRPCTransport{Target: ln.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := tr.Connect(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error")
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("Connect returned after %v with a 300ms ctx", d)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("Connect still blocked after 25s with a 300ms ctx")
	}
}

// m2: passing any DialOption (here only WithUserAgent) must not disable the
// insecure-credentials default. Today hasTransportCredentials treats "any
// option" as "has credentials" and grpc.NewClient fails with
// "no transport security set".
func TestAudit_m2_DialOptionsWithoutCredentials(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "audit", Version: "0"}, nil)
	gs := grpc.NewServer()
	emcpv1.RegisterMCPTransportServer(gs, NewGRPCHandler(func() *mcp.Server { return s }))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(gs.Stop)

	tr := &GRPCTransport{Target: ln.Addr().String(), DialOptions: []grpc.DialOption{grpc.WithUserAgent("emcp-audit")}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := tr.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect with only WithUserAgent: %v", err)
	}
	_ = conn.Close()
}
