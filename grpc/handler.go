// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"strings"

	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCHandler implements emcpv1.MCPTransportServer. It bridges incoming gRPC
// bidirectional streams to mcp.Server sessions.
//
// Each Stream call creates a new mcp.Connection over the gRPC stream, wraps
// it in a singleConnTransport, and connects it to the MCP server. The server
// handles JSON-RPC messages for the lifetime of the stream.
//
// Usage:
//
//	server := mcp.NewServer(impl, nil)
//	handler := grpc.NewGRPCHandler(func() *mcp.Server { return server })
//	grpcServer := grpc.NewServer()
//	emcpv1.RegisterMCPTransportServer(grpcServer, handler)
type GRPCHandler struct {
	emcpv1.UnimplementedMCPTransportServer

	// getServer returns the MCP server for a new stream. It is called once
	// per incoming Stream RPC. Returning the same *mcp.Server is fine — the
	// SDK manages per-session state internally.
	//
	// This follows the same pattern as StreamableHTTPHandler.getServer.
	getServer func() *mcp.Server

	// tokenValidator, when non-nil, enforces bearer token authentication
	// inside Stream() itself. This ensures gRPC auth is enforced even when
	// the consumer forgets to register GRPCAuthInterceptor on the grpc.Server.
	// The validator is called with the raw token (without "Bearer " prefix);
	// it returns true if the token is valid.
	tokenValidator func(token string) bool
}

// NewGRPCHandler creates a GRPCHandler that delegates to the server returned
// by getServer. The getServer function is called once per incoming stream.
// It is safe (and typical) for getServer to return the same *mcp.Server
// instance — the SDK creates a new ServerSession per transport connection.
func NewGRPCHandler(getServer func() *mcp.Server) *GRPCHandler {
	return &GRPCHandler{getServer: getServer}
}

// NewGRPCHandlerWithAuth creates a GRPCHandler with built-in bearer token
// validation. When tokenValidator is non-nil, every incoming Stream is
// authenticated before any MCP processing begins — regardless of whether
// the consumer registered GRPCAuthInterceptor on the grpc.Server.
func NewGRPCHandlerWithAuth(getServer func() *mcp.Server, tokenValidator func(string) bool) *GRPCHandler {
	return &GRPCHandler{
		getServer:      getServer,
		tokenValidator: tokenValidator,
	}
}

// Stream implements emcpv1.MCPTransportServer. It bridges a single gRPC
// bidirectional stream to an MCP session. The method blocks until the stream
// is closed by either side.
func (h *GRPCHandler) Stream(stream emcpv1.MCPTransport_StreamServer) error {
	// B8: enforce token auth inside Stream() itself. When tokenValidator is
	// set, gRPC calls are rejected with Unauthenticated if the bearer token
	// is missing or invalid — regardless of whether the consumer registered
	// GRPCAuthInterceptor on the grpc.Server.
	if h.tokenValidator != nil {
		md, ok := metadata.FromIncomingContext(stream.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		vals := md.Get("authorization")
		if len(vals) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization metadata")
		}
		token := vals[0]
		const prefix = "Bearer "
		if !strings.HasPrefix(token, prefix) {
			return status.Error(codes.Unauthenticated, "authorization must use Bearer scheme")
		}
		if !h.tokenValidator(strings.TrimPrefix(token, prefix)) {
			return status.Error(codes.Unauthenticated, "invalid bearer token")
		}
	}

	server := h.getServer()
	if server == nil {
		return ErrNoServer
	}

	conn := newServerConnection(stream)
	transport := &singleConnTransport{conn: conn}

	session, err := server.Connect(stream.Context(), transport, nil)
	if err != nil {
		_ = conn.Close()
		return err
	}

	// Wait for the session to end. This happens when:
	// - The client closes the stream (Read returns io.EOF)
	// - The server context is cancelled
	// - An error occurs on the stream
	_ = session.Wait()
	return nil
}

// singleConnTransport adapts a pre-existing mcp.Connection into a one-shot
// mcp.Transport. Server.Connect calls Transport.Connect exactly once, so this
// simply returns the wrapped connection.
type singleConnTransport struct {
	conn mcp.Connection
}

// Compile-time check: singleConnTransport implements mcp.Transport.
var _ mcp.Transport = (*singleConnTransport)(nil)

// Connect returns the wrapped connection. It is called exactly once by
// mcp.Server.Connect.
func (t *singleConnTransport) Connect(context.Context) (mcp.Connection, error) {
	return t.conn, nil
}
