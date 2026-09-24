// Deprecated: Package client is part of eMCP v0.1 and will be removed.
// Use github.com/modelcontextprotocol/go-sdk/mcp for MCP client functionality,
// and github.com/goco-ai/emcp-go/daemontx for daemon transport.
package client

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/goco-ai/emcp-go/emcp"
)

// Client implements an Enhanced MCP client with enterprise features.
type Client struct {
	// Configuration
	info emcp.ClientInfo

	// Transport
	transport Transport

	// State management
	mu            sync.RWMutex
	initialized   bool
	serverInfo    *emcp.ServerInfo
	tools         []emcp.ToolDefinition
	requestID     atomic.Uint64
	pendingCalls  map[string]chan response
	pendingCallsMu sync.Mutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// Transport defines the interface for client transports
type Transport interface {
	Send(msg []byte) error
	Receive() ([]byte, error)
	Close() error
}

// response represents a pending call response
type response struct {
	result []byte
	err    error
}

// Option configures a Client
type Option func(*Client)

// New creates a new eMCP client with the given transport
func New(transport Transport, opts ...Option) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		info: emcp.ClientInfo{
			Name:    "emcp-client",
			Version: emcp.Version,
		},
		transport:    transport,
		pendingCalls: make(map[string]chan response),
		ctx:          ctx,
		cancel:       cancel,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Start response handler
	go c.handleResponses()

	return c
}

// WithClientInfo sets custom client info
func WithClientInfo(name, version string) Option {
	return func(c *Client) {
		c.info.Name = name
		c.info.Version = version
	}
}

// Initialize performs MCP initialization handshake
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return fmt.Errorf("client already initialized")
	}

	// Build initialize request
	req := map[string]any{
		"jsonrpc": emcp.JSONRPCVersion,
		"id":      c.nextRequestID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": emcp.ProtocolVersion,
			"clientInfo":      c.info,
			"capabilities":    map[string]any{},
		},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	respCh := make(chan response, 1)
	reqID := req["id"].(string)

	c.registerCall(reqID, respCh)
	defer c.unregisterCall(reqID)

	if err := c.transport.Send(reqData); err != nil {
		return fmt.Errorf("failed to send initialize: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.err != nil {
			return resp.err
		}

		var result emcp.InitializeResult
		if err := json.Unmarshal(resp.result, &result); err != nil {
			return fmt.Errorf("failed to parse initialize result: %w", err)
		}

		c.serverInfo = &result.ServerInfo
		c.initialized = true
		return nil

	case <-ctx.Done():
		return ctx.Err()
	}
}

// ListTools retrieves available tools from server
func (c *Client) ListTools(ctx context.Context) ([]emcp.ToolDefinition, error) {
	c.mu.RLock()
	if !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	c.mu.RUnlock()

	// Build request
	req := map[string]any{
		"jsonrpc": emcp.JSONRPCVersion,
		"id":      c.nextRequestID(),
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	respCh := make(chan response, 1)
	c.registerCall(req["id"].(string), respCh)
	defer c.unregisterCall(req["id"].(string))

	if err := c.transport.Send(reqData); err != nil {
		return nil, fmt.Errorf("failed to send tools/list: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.err != nil {
			return nil, resp.err
		}

		var result struct {
			Tools []emcp.ToolDefinition `json:"tools"`
		}
		if err := json.Unmarshal(resp.result, &result); err != nil {
			return nil, fmt.Errorf("failed to parse tools/list result: %w", err)
		}

		c.mu.Lock()
		c.tools = result.Tools
		c.mu.Unlock()

		return result.Tools, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CallTool executes a tool on the server
func (c *Client) CallTool(ctx context.Context, name string, arguments any) ([]byte, error) {
	c.mu.RLock()
	if !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	c.mu.RUnlock()

	// Build request
	req := map[string]any{
		"jsonrpc": emcp.JSONRPCVersion,
		"id":      c.nextRequestID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	respCh := make(chan response, 1)
	c.registerCall(req["id"].(string), respCh)
	defer c.unregisterCall(req["id"].(string))

	if err := c.transport.Send(reqData); err != nil {
		return nil, fmt.Errorf("failed to send tools/call: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.err != nil {
			return nil, resp.err
		}

		return resp.result, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Ping sends a ping request to verify connection
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	if !c.initialized {
		c.mu.RUnlock()
		return fmt.Errorf("client not initialized")
	}
	c.mu.RUnlock()

	// Build request
	req := map[string]any{
		"jsonrpc": emcp.JSONRPCVersion,
		"id":      c.nextRequestID(),
		"method":  "ping",
		"params":  map[string]any{},
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	respCh := make(chan response, 1)
	c.registerCall(req["id"].(string), respCh)
	defer c.unregisterCall(req["id"].(string))

	if err := c.transport.Send(reqData); err != nil {
		return fmt.Errorf("failed to send ping: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		return resp.err

	case <-ctx.Done():
		return ctx.Err()
	}
}

// ServerInfo returns information about the connected server
func (c *Client) ServerInfo() *emcp.ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// Close gracefully closes the client connection
func (c *Client) Close() error {
	c.cancel()
	return c.transport.Close()
}

// handleResponses processes incoming messages from transport
func (c *Client) handleResponses() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			msg, err := c.transport.Receive()
			if err != nil {
				// Transport closed or error
				return
			}

			c.processMessage(msg)
		}
	}
}

// processMessage handles a single incoming message
func (c *Client) processMessage(data []byte) {
	var base struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Result  any `json:"result,omitempty"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    any    `json:"data,omitempty"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(data, &base); err != nil {
		// Invalid message, ignore
		return
	}

	// Convert ID to string
	var idStr string
	if base.ID != nil {
		idStr = fmt.Sprintf("%v", base.ID)
	}

	// Find pending call
	c.pendingCallsMu.Lock()
	respCh, exists := c.pendingCalls[idStr]
	c.pendingCallsMu.Unlock()

	if !exists {
		// No pending call for this ID
		return
	}

	// Send response
	if base.Error != nil {
		respCh <- response{
			err: fmt.Errorf("RPC error %d: %s", base.Error.Code, base.Error.Message),
		}
	} else {
		// Marshal result back to bytes
		resultBytes, err := json.Marshal(base.Result)
		if err != nil {
			respCh <- response{
				err: fmt.Errorf("failed to marshal result: %w", err),
			}
		} else {
			respCh <- response{
				result: resultBytes,
			}
		}
	}
}

// nextRequestID generates a unique request ID
func (c *Client) nextRequestID() string {
	id := c.requestID.Add(1)
	return fmt.Sprintf("%d", id)
}

// registerCall registers a pending call
func (c *Client) registerCall(id string, ch chan response) {
	c.pendingCallsMu.Lock()
	c.pendingCalls[id] = ch
	c.pendingCallsMu.Unlock()
}

// unregisterCall removes a pending call
func (c *Client) unregisterCall(id string) {
	c.pendingCallsMu.Lock()
	delete(c.pendingCalls, id)
	c.pendingCallsMu.Unlock()
}