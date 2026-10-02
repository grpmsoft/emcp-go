// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Package mcpclient implements the MCP client logic. It is internal to
// emcp-go; external consumers use the thin wrapper in the root emcp package.
package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	emcpgrpc "github.com/grpmsoft/emcp-go/grpc"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Transport selects the transport protocol.
type Transport string

const (
	TransportHTTP  Transport = "http"
	TransportGRPC  Transport = "grpc"
	TransportStdio Transport = "stdio"
	TransportAuto  Transport = "auto"
)

// Config holds all parameters needed to connect a Client to an MCP server.
// This mirrors the public emcp.Config but lives in internal to avoid
// circular imports.
type Config struct {
	Endpoint    string
	PIDFile     string
	Transport   Transport
	Command     string
	Args        []string
	BearerToken string
	Timeout     time.Duration
}

// Client is a unified MCP client that supports HTTP, gRPC, and stdio
// transports. It manages the underlying SDK session internally and
// reconnects transparently if the session dies.
type Client struct {
	config  Config
	mu      sync.Mutex
	session *mcp.ClientSession
	client  *mcp.Client
	closed  bool

	// done is closed when the current session ends (the session's Wait
	// returns). Ping checks this channel to detect a dead session without
	// spawning a new process or making a network call.
	done chan struct{}
}

// New creates a new MCP client. The caller is responsible for resolving
// PID files and validating the config before calling New. The client
// eagerly connects to the server.
func New(cfg Config) (*Client, error) {
	if cfg.Transport == "" {
		cfg.Transport = TransportAuto
	}

	if cfg.Endpoint == "" && cfg.Command == "" {
		return nil, fmt.Errorf("emcp: either Endpoint, PIDFile, or Command must be set")
	}

	c := &Client{
		config: cfg,
		client: mcp.NewClient(
			&mcp.Implementation{Name: "emcp-client", Version: "1.0.0"},
			nil,
		),
	}

	// Apply the configured timeout to the initial connection attempt.
	// Without this, a silent server would cause NewClient to hang forever
	// on context.Background().
	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

// connect establishes a new session. Caller must hold c.mu or be in New().
func (c *Client) connect(ctx context.Context) error {
	transport, err := c.makeTransport()
	if err != nil {
		return err
	}

	session, err := c.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("emcp: connect failed: %w", err)
	}
	c.session = session

	// Start a goroutine that watches for session completion. When the
	// session ends (child process dies, stream closes, server disconnect),
	// the done channel is closed. Ping checks this to detect a dead session
	// without spawning new processes or making network calls.
	done := make(chan struct{})
	c.done = done
	go func() {
		_ = session.Wait()
		close(done)
	}()

	return nil
}

// makeTransport creates the appropriate MCP transport based on config.
func (c *Client) makeTransport() (mcp.Transport, error) {
	switch c.config.Transport {
	case TransportHTTP:
		return c.makeHTTPTransport(), nil
	case TransportGRPC:
		return c.makeGRPCTransport(), nil
	case TransportAuto:
		return c.makeAutoTransport()
	case TransportStdio:
		return c.makeStdioTransport()
	default:
		return nil, fmt.Errorf("emcp: unknown transport %q", c.config.Transport)
	}
}

// makeStdioTransport creates a CommandTransport for stdio-based MCP servers.
func (c *Client) makeStdioTransport() (mcp.Transport, error) {
	if c.config.Command == "" {
		return nil, fmt.Errorf("emcp: stdio transport requires Command to be set")
	}
	return &mcp.CommandTransport{
		Command: exec.Command(c.config.Command, c.config.Args...),
	}, nil
}

// makeAutoTransport selects the transport based on the endpoint format:
//   - starts with "http://" or "https://" -> HTTP
//   - bare "host:port" (no scheme, no path) -> gRPC
//   - default -> HTTP
func (c *Client) makeAutoTransport() (mcp.Transport, error) {
	ep := c.config.Endpoint
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		return c.makeHTTPTransport(), nil
	}
	// Bare host:port (contains ":" but no "/" indicating a path or scheme) -> gRPC.
	if strings.Contains(ep, ":") && !strings.Contains(ep, "/") {
		return c.makeGRPCTransport(), nil
	}
	return c.makeHTTPTransport(), nil
}

// makeHTTPTransport creates a StreamableClientTransport with optional bearer auth.
//
// B3 fix: We do NOT set http.Client.Timeout because it applies to the entire
// request lifetime, including long-lived SSE GET streams on stateful servers.
// That kills the SSE connection after Timeout elapses, breaking stateful clients.
//
// Instead, when Config.Timeout is set, we configure the underlying HTTP
// Transport with DialContext timeout and ResponseHeaderTimeout. This bounds:
//   - TCP dial time (hanging server that never accepts)
//   - HTTP response header wait (server accepts TCP but never responds)
//
// Once headers are received (SSE stream starts), the stream lives until the
// session is closed. MaxRetries is left at the SDK default so the SDK can
// reopen SSE streams on transient failures.
func (c *Client) makeHTTPTransport() *mcp.StreamableClientTransport {
	t := &mcp.StreamableClientTransport{
		Endpoint: c.config.Endpoint,
	}

	// Build a custom HTTP client when we need bearer auth or a connect timeout.
	rt := http.RoundTripper(http.DefaultTransport)
	if c.config.Timeout > 0 {
		rt = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: c.config.Timeout,
			}).DialContext,
			ResponseHeaderTimeout: c.config.Timeout,
			// Preserve sensible defaults from http.DefaultTransport.
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}
	if c.config.BearerToken != "" {
		rt = &bearerRoundTripper{
			token: c.config.BearerToken,
			next:  rt,
		}
	}
	if c.config.Timeout > 0 || c.config.BearerToken != "" {
		t.HTTPClient = &http.Client{Transport: rt}
	}

	return t
}

// makeGRPCTransport creates a GRPCTransport for the configured endpoint.
// When a BearerToken is configured, per-RPC credentials are injected so
// the token is sent as "authorization: Bearer <token>" metadata on every
// gRPC call.
func (c *Client) makeGRPCTransport() *emcpgrpc.GRPCTransport {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if c.config.BearerToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearerCredentials{token: c.config.BearerToken}))
	}
	return &emcpgrpc.GRPCTransport{
		Target:      c.config.Endpoint,
		DialOptions: opts,
	}
}

// bearerCredentials implements grpc credentials.PerRPCCredentials to inject
// a Bearer token into the metadata of every gRPC call.
type bearerCredentials struct {
	token string
}

func (b bearerCredentials) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

// RequireTransportSecurity returns false because the daemon use case runs
// over plaintext loopback. In production deployments with TLS this is still
// safe: TLS protects the token on the wire regardless of this flag.
func (b bearerCredentials) RequireTransportSecurity() bool { return false }

// ensureSession returns the active session, reconnecting if necessary.
func (c *Client) ensureSession(ctx context.Context) (*mcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("emcp: client is closed")
	}

	if c.session != nil {
		return c.session, nil
	}

	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	return c.session, nil
}

// withTimeout wraps ctx with the configured per-call timeout, if any.
func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.config.Timeout > 0 {
		return context.WithTimeout(ctx, c.config.Timeout)
	}
	return ctx, func() {}
}

// ListTools returns the raw SDK ListToolsResult from the connected MCP server.
func (c *Client) ListTools(ctx context.Context) (*mcp.ListToolsResult, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	session, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		if isTransportError(err) {
			c.invalidateSession()
		}
		return nil, fmt.Errorf("emcp: ListTools failed: %w", err)
	}

	return result, nil
}

// CallTool invokes a tool by name with the given arguments and returns the
// raw SDK CallToolResult. On transport errors (server restart, dead stream),
// it invalidates the session. It retries once ONLY when the error indicates
// the request could not have reached the server (connection refused, session
// missing, connection closed before the call started). Mid-flight errors
// (connection reset, broken pipe, EOF after the request was sent) are NOT
// retried to prevent double-execution of non-idempotent tools.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	session, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		if isTransportError(err) {
			c.invalidateSession()
			// Retry once ONLY if the error proves the request never reached
			// the server. Mid-flight errors (connection reset, broken pipe,
			// EOF) could mean the server executed the tool but crashed before
			// delivering the response -- retrying would double-execute.
			if isRetryableError(err) {
				session2, err2 := c.ensureSession(ctx)
				if err2 != nil {
					return nil, fmt.Errorf("emcp: CallTool %q failed: %w", name, err)
				}
				result2, err2 := session2.CallTool(ctx, &mcp.CallToolParams{
					Name:      name,
					Arguments: args,
				})
				if err2 != nil {
					if isTransportError(err2) {
						c.invalidateSession()
					}
					return nil, fmt.Errorf("emcp: CallTool %q failed: %w", name, err2)
				}
				return result2, nil
			}
		}
		return nil, fmt.Errorf("emcp: CallTool %q failed: %w", name, err)
	}

	return result, nil
}

// Ping verifies the server is alive. On MCP 2026-07-28 (stateless) servers,
// the "ping" method is removed. In that case, Ping performs a throw-away
// Connect+Close cycle (one server/discover POST) as a liveness probe.
// On legacy (2025-11-25) servers, the SDK session.Ping is used directly.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	session, err := c.ensureSession(ctx)
	if err != nil {
		return err
	}

	// Check the negotiated protocol version. On 2026-07-28+, ping is gone;
	// use a lightweight server/discover round-trip instead.
	if ir := session.InitializeResult(); ir != nil && ir.ProtocolVersion >= "2026-07-28" {
		return c.pingViaDiscover(ctx)
	}

	if err := session.Ping(ctx, nil); err != nil {
		if isTransportError(err) {
			c.invalidateSession()
		}
		return fmt.Errorf("emcp: Ping failed: %w", err)
	}
	return nil
}

// pingViaDiscover performs a liveness check appropriate for the transport.
//
// B6 fix: for stdio, makeTransport() spawns a new child process via
// exec.Command, so we must NOT call it for ping. Instead, the existing
// session IS the liveness proof for stdio (if the child died, the next
// call will fail and trigger reconnect).
//
// For HTTP and gRPC, we do a throw-away Connect+Close (one server/discover
// round-trip) to verify the server is reachable.
func (c *Client) pingViaDiscover(ctx context.Context) error {
	// For ALL transports, check the done channel first. When the session
	// has ended (child process died, stream closed, server disconnect),
	// the done channel is closed by the watcher goroutine in connect().
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done != nil {
		select {
		case <-done:
			return fmt.Errorf("emcp: session closed")
		default:
		}
	}

	// For stdio, the done-channel check above is the only liveness probe.
	// We must NOT call makeTransport() for stdio because it would spawn
	// a new child process (B6 fix).
	if c.config.Transport == TransportStdio {
		return nil
	}

	transport, err := c.makeTransport()
	if err != nil {
		return fmt.Errorf("emcp: Ping failed: %w", err)
	}
	probe := mcp.NewClient(
		&mcp.Implementation{Name: "emcp-ping-probe", Version: "1.0.0"},
		nil,
	)
	sess, err := probe.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("emcp: Ping failed: %w", err)
	}
	_ = sess.Close()
	return nil
}

// Close shuts down the client and releases all resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.session != nil {
		err := c.session.Close()
		c.session = nil
		return err
	}
	return nil
}

// ClearSession clears the internal session, forcing a reconnect on the next
// call. This is exported for test support (the root package test accesses
// this to simulate session loss).
func (c *Client) ClearSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
}

// isTransportError returns true when the error indicates the session's
// underlying transport is broken and the session must be discarded.
// JSON-RPC application errors (tool not found, invalid params, handler
// errors) return false -- the session is still usable.
//
// B5 fix: context.DeadlineExceeded and context.Canceled are NOT transport
// errors. A slow tool hitting a per-call deadline does not mean the server
// is dead -- it means this particular call timed out. Killing the session
// on every timeout causes all concurrent callers to fail and forces
// unnecessary reconnects.
func isTransportError(err error) bool {
	if err == nil {
		return false
	}

	// JSON-RPC structured errors mean the server processed our request and
	// replied with a protocol-level error. The transport is fine.
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		return false
	}

	// Context errors are NOT transport errors. A per-call deadline or
	// cancellation does not indicate the server is dead.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}

	// SDK sentinel errors for broken connections/sessions.
	if errors.Is(err, mcp.ErrConnectionClosed) || errors.Is(err, mcp.ErrSessionMissing) {
		return true
	}

	// Underlying I/O errors (EOF, connection reset, etc).
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Common connection-level error patterns. These happen when the TCP
	// connection is broken (server died, network partition, etc).
	msg := err.Error()
	for _, needle := range []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"use of closed network connection",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}

	return false
}

// isRetryableError returns true only when the error proves the request never
// reached the server. Only these errors are safe for automatic retry in
// CallTool, because retrying a request that DID reach the server could
// double-execute a non-idempotent tool (e.g., apply_patch).
//
// Mid-flight errors like "connection reset", "broken pipe", and EOF mean the
// request may have been received and executed before the connection dropped.
// Those are transport errors (session should be invalidated) but NOT retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// SDK sentinel errors that prove no request was processed.
	if errors.Is(err, mcp.ErrSessionMissing) || errors.Is(err, mcp.ErrConnectionClosed) {
		return true
	}

	// Connection refused: the server wasn't even listening.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			if sysErr.Syscall == "connectex" || sysErr.Syscall == "connect" {
				return true
			}
		}
	}

	// Fallback string check for "connection refused" across platforms.
	if strings.Contains(err.Error(), "connection refused") {
		return true
	}

	return false
}

// invalidateSession closes the current session and clears it so the next
// call will trigger a reconnect.
//
// B4 fix: session.Close() sends a DELETE request on legacy (stateful)
// sessions. If the server hangs on DELETE, holding c.mu during Close()
// would block ALL concurrent callers. We copy the session pointer under
// the lock, release the lock, then Close() outside the lock.
func (c *Client) invalidateSession() {
	c.mu.Lock()
	s := c.session
	c.session = nil
	c.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

// bearerRoundTripper injects a Bearer token into every HTTP request.
type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (rt *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.next.RoundTrip(req)
}
