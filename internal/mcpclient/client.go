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
	"net/http"
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

// connect establishes a new session. Caller must hold c.mu.
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
func (c *Client) makeHTTPTransport() *mcp.StreamableClientTransport {
	t := &mcp.StreamableClientTransport{
		Endpoint: c.config.Endpoint,
		// Disable SDK retries — they defeat our timeout by retrying
		// multiple times against a non-responsive server.
		MaxRetries: -1,
	}

	// Build a custom HTTP client when we need bearer auth or a timeout.
	var rt http.RoundTripper = http.DefaultTransport
	if c.config.BearerToken != "" {
		rt = &bearerRoundTripper{
			token: c.config.BearerToken,
			next:  rt,
		}
	}
	if c.config.BearerToken != "" || c.config.Timeout > 0 {
		httpClient := &http.Client{Transport: rt}
		if c.config.Timeout > 0 {
			httpClient.Timeout = c.config.Timeout
		}
		t.HTTPClient = httpClient
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
// raw SDK CallToolResult.
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

// pingViaDiscover performs a throw-away Connect+Close as a liveness check.
// This sends a single server/discover POST and tears down the temporary
// session immediately.
func (c *Client) pingViaDiscover(ctx context.Context) error {
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
// errors) return false — the session is still usable.
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

	// SDK sentinel errors for broken connections/sessions.
	if errors.Is(err, mcp.ErrConnectionClosed) || errors.Is(err, mcp.ErrSessionMissing) {
		return true
	}

	// Underlying I/O errors (EOF, connection reset, etc).
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Context errors propagated from a failed transport read/write.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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

// invalidateSession closes the current session and clears it so the next
// call will trigger a reconnect. Caller must NOT hold c.mu.
func (c *Client) invalidateSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
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
