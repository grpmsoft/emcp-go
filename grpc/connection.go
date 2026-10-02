// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	emcpv1 "github.com/goco-ai/emcp-go/grpc/proto/emcpv1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Compile-time check: grpcConnection implements mcp.Connection.
var _ mcp.Connection = (*grpcConnection)(nil)

// streamSender is the subset of a gRPC stream used by grpcConnection for sending.
// Both MCPTransport_StreamClient and MCPTransport_StreamServer satisfy this.
type streamSender interface {
	Send(*emcpv1.MCPMessage) error
}

// streamReceiver is the subset of a gRPC stream used by grpcConnection for receiving.
// Both MCPTransport_StreamClient and MCPTransport_StreamServer satisfy this.
type streamReceiver interface {
	Recv() (*emcpv1.MCPMessage, error)
}

// streamCloser is optionally implemented by client-side streams to signal
// that no more messages will be sent. Server-side streams do not support
// CloseSend, so this is checked at runtime.
type streamCloser interface {
	CloseSend() error
}

// grpcConnection bridges a gRPC bidirectional stream to the mcp.Connection
// interface. It serializes JSON-RPC messages via jsonrpc.EncodeMessage /
// DecodeMessage and transports them as opaque byte payloads inside
// emcpv1.MCPMessage frames.
type grpcConnection struct {
	sender   streamSender
	receiver streamReceiver

	// writeMu guards Send, which must be concurrent-safe per Connection contract.
	writeMu sync.Mutex

	// closed is signaled (by closing the channel) when Close is called.
	// Read selects on this to unblock when the connection shuts down.
	closed    chan struct{}
	closeOnce sync.Once

	// incoming buffers messages received from the gRPC stream in a background
	// goroutine so that Read can select on both incoming data and Close.
	incoming chan msgOrErr
}

// msgOrErr carries a decoded JSON-RPC message or the error from Recv.
type msgOrErr struct {
	msg jsonrpc.Message
	err error
}

// newClientConnection creates a grpcConnection for the client side.
func newClientConnection(stream emcpv1.MCPTransport_StreamClient) *grpcConnection {
	return newGRPCConnection(stream, stream)
}

// newServerConnection creates a grpcConnection for the server side.
func newServerConnection(stream emcpv1.MCPTransport_StreamServer) *grpcConnection {
	return newGRPCConnection(stream, stream)
}

// newGRPCConnection wires a sender/receiver pair into a grpcConnection and
// starts the background read loop.
func newGRPCConnection(sender streamSender, receiver streamReceiver) *grpcConnection {
	c := &grpcConnection{
		sender:   sender,
		receiver: receiver,
		closed:   make(chan struct{}),
		incoming: make(chan msgOrErr, 16),
	}
	go c.readLoop()
	return c
}

// readLoop receives MCPMessage frames from the gRPC stream, decodes them,
// and pushes the result into the incoming channel. It stops when the stream
// returns an error (including io.EOF) or when the connection is closed.
func (c *grpcConnection) readLoop() {
	for {
		pbMsg, err := c.receiver.Recv()
		if err != nil {
			// Normalize gRPC stream-end to io.EOF for the MCP layer.
			select {
			case c.incoming <- msgOrErr{err: err}:
			case <-c.closed:
			}
			return
		}

		msg, err := jsonrpc.DecodeMessage(pbMsg.GetPayload())
		if err != nil {
			select {
			case c.incoming <- msgOrErr{err: fmt.Errorf("failed to decode JSON-RPC message: %w", err)}:
			case <-c.closed:
			}
			return
		}

		select {
		case c.incoming <- msgOrErr{msg: msg}:
		case <-c.closed:
			return
		}
	}
}

// Read returns the next JSON-RPC message from the gRPC stream.
// It blocks until a message arrives, the context is cancelled, or
// the connection is closed.
func (c *grpcConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, io.EOF
	case moe := <-c.incoming:
		return moe.msg, moe.err
	}
}

// Write serializes msg via jsonrpc.EncodeMessage and sends it over the gRPC
// stream. Write is concurrent-safe as required by the mcp.Connection contract.
func (c *grpcConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return mcp.ErrConnectionClosed
	default:
	}

	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to encode JSON-RPC message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Re-check after acquiring the lock.
	select {
	case <-c.closed:
		return mcp.ErrConnectionClosed
	default:
	}

	if err := c.sender.Send(&emcpv1.MCPMessage{Payload: data}); err != nil {
		return fmt.Errorf("failed to send gRPC message: %w", err)
	}
	return nil
}

// Close closes the connection. It signals the closed channel, which unblocks
// any pending Read. If the underlying stream supports CloseSend (client-side),
// it is called to cleanly half-close the stream.
//
// Close is idempotent and concurrency-safe.
func (c *grpcConnection) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		// Client streams support CloseSend to signal no more messages.
		if cs, ok := c.sender.(streamCloser); ok {
			_ = cs.CloseSend()
		}
	})
	return nil
}

// SessionID returns an empty string. gRPC stream transports do not use
// session identifiers — the stream itself IS the session.
func (c *grpcConnection) SessionID() string {
	return ""
}
