// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"

	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
}

// NewGRPCHandler creates a GRPCHandler that delegates to the server returned
// by getServer. The getServer function is called once per incoming stream.
// It is safe (and typical) for getServer to return the same *mcp.Server
// instance — the SDK creates a new ServerSession per transport connection.
func NewGRPCHandler(getServer func() *mcp.Server) *GRPCHandler {
	return &GRPCHandler{getServer: getServer}
}

// Stream implements emcpv1.MCPTransportServer. It bridges a single gRPC
// bidirectional stream to an MCP session. The method blocks until the stream
// is closed by either side.
func (h *GRPCHandler) Stream(stream emcpv1.MCPTransport_StreamServer) error {
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
