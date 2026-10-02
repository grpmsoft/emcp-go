// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"fmt"

	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Compile-time check: GRPCTransport implements mcp.Transport.
var _ mcp.Transport = (*GRPCTransport)(nil)

// GRPCTransport is a client-side MCP transport that communicates over a gRPC
// bidirectional stream. Use it with mcp.Client.Connect to establish an MCP
// session over gRPC.
//
// Example:
//
//	transport := &grpc.GRPCTransport{
//	    Target:      "localhost:50051",
//	    DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
//	}
//	client := mcp.NewClient(impl, nil)
//	session, err := client.Connect(ctx, transport, nil)
type GRPCTransport struct {
	// Target is the gRPC server address (e.g. "localhost:50051").
	Target string

	// DialOptions are additional gRPC dial options. If no transport credentials
	// are provided, insecure credentials are used by default.
	DialOptions []grpc.DialOption

	// conn is used in tests to inject a pre-dialed connection (e.g. bufconn).
	// When set, Target and DialOptions are ignored.
	conn grpc.ClientConnInterface
}

// Connect dials the gRPC server, opens a bidirectional MCPTransport.Stream,
// and returns an mcp.Connection backed by the gRPC stream.
//
// The underlying grpc.ClientConn is closed when the returned Connection is
// closed. Connect should be called exactly once per transport instance.
//
// IMPORTANT: The provided ctx is used only for dialing and opening the stream.
// The stream itself lives until the returned Connection is closed. This is
// necessary because the MCP SDK may cancel the Connect context after the
// session handshake completes; the stream must outlive that cancellation.
func (t *GRPCTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	var (
		grpcConn grpc.ClientConnInterface
		ownedCC  *grpc.ClientConn // non-nil when we dialed and must close on error
	)

	if t.conn != nil {
		// Test path: use the injected connection.
		grpcConn = t.conn
	} else {
		opts := t.DialOptions
		if !hasTransportCredentials(opts) {
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}
		cc, err := grpc.NewClient(t.Target, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to dial gRPC target %q: %w", t.Target, err)
		}
		grpcConn = cc
		ownedCC = cc
	}

	client := emcpv1.NewMCPTransportClient(grpcConn)

	// Use a long-lived context for the stream. The gRPC stream must outlive
	// the Connect call — it stays open for the entire MCP session. We create
	// a cancellable context that is cancelled when the connection is closed.
	streamCtx, streamCancel := context.WithCancel(context.Background())

	stream, err := client.Stream(streamCtx)
	if err != nil {
		streamCancel()
		if ownedCC != nil {
			_ = ownedCC.Close()
		}
		return nil, fmt.Errorf("failed to open gRPC stream: %w", err)
	}

	conn := newClientConnection(stream)

	// If we own the grpc.ClientConn, wrap Close to also cancel the stream
	// context and tear down the grpc.ClientConn.
	if ownedCC != nil {
		return &ownedClientConn{
			grpcConnection: conn,
			cc:             ownedCC,
			cancelStream:   streamCancel,
		}, nil
	}
	return &cancellingConn{
		grpcConnection: conn,
		cancelStream:   streamCancel,
	}, nil
}

// cancellingConn wraps a grpcConnection to also cancel the stream context
// when the connection is closed. Used when the caller provides their own
// grpc.ClientConn (e.g. in tests via the conn field).
type cancellingConn struct {
	*grpcConnection
	cancelStream context.CancelFunc
}

func (c *cancellingConn) Close() error {
	err := c.grpcConnection.Close()
	c.cancelStream()
	return err
}

// ownedClientConn wraps a grpcConnection to also cancel the stream context
// and close the grpc.ClientConn that was dialed by GRPCTransport.Connect.
type ownedClientConn struct {
	*grpcConnection
	cc           *grpc.ClientConn
	cancelStream context.CancelFunc
}

func (c *ownedClientConn) Close() error {
	connErr := c.grpcConnection.Close()
	c.cancelStream()
	ccErr := c.cc.Close()
	if connErr != nil {
		return connErr
	}
	return ccErr
}

// hasTransportCredentials checks whether any of the dial options already
// configure transport credentials. This avoids double-applying insecure creds
// when the caller has already set them.
func hasTransportCredentials(opts []grpc.DialOption) bool {
	// grpc.DialOption is an opaque interface — we cannot inspect its contents.
	// A simple heuristic: if the caller passed any options at all, assume they
	// know what they are doing. If they passed none, default to insecure.
	// This is safe because grpc.NewClient requires credentials and will fail
	// with a clear error if none are set.
	for _, opt := range opts {
		if opt != nil {
			return true
		}
	}
	return false
}
