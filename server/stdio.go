// Package server provides stdio transport for MCP protocol compatibility
package server

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/goco-ai/emcp-go/emcp"
)

// StdioTransport implements stdio-based JSON-RPC transport for MCP 1.0 compatibility
type StdioTransport struct {
	server  *Server
	reader  *bufio.Reader
	writer  *bufio.Writer
	mu      sync.Mutex
	verbose bool // Enable verbose logging for debugging

	// Graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(server *Server) *StdioTransport {
	ctx, cancel := context.WithCancel(context.Background())
	// Enable verbose by default via environment variable
	verbose := os.Getenv("GODA_MCP_DEBUG") == "true"
	return &StdioTransport{
		server:  server,
		reader:  bufio.NewReader(os.Stdin),
		writer:  bufio.NewWriter(os.Stdout),
		ctx:     ctx,
		cancel:  cancel,
		verbose: verbose,
	}
}

// SetVerbose enables or disables verbose logging
func (t *StdioTransport) SetVerbose(verbose bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.verbose = verbose
}

// logf logs a message to stderr if verbose is enabled
func (t *StdioTransport) logf(format string, args ...interface{}) {
	if t.verbose {
		log.Printf("[eMCP stdio] "+format, args...)
	}
}

// Serve starts the stdio transport loop
func (t *StdioTransport) Serve() error {
	t.logf("Stdio transport starting...")
	t.wg.Add(1)
	defer t.wg.Done()

	for {
		select {
		case <-t.ctx.Done():
			t.logf("Context cancelled, stopping server")
			return t.ctx.Err()
		default:
			// Read one line (JSON-RPC message)
			t.logf("Waiting for message from stdin...")
			line, err := t.reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					t.logf("EOF received, client disconnected cleanly")
					return nil
				}
				// CRITICAL: Log the actual error before failing
				log.Printf("[eMCP stdio] CRITICAL: Read error: %v", err)
				return fmt.Errorf("read error: %w", err)
			}

			t.logf("Received message: %s", string(line))

			// Handle message in goroutine for concurrent processing
			t.wg.Add(1)
			go func(msg []byte) {
				defer t.wg.Done()
				t.handleMessage(t.ctx, msg)
			}(line)
		}
	}
}

// handleMessage processes a single JSON-RPC message
func (t *StdioTransport) handleMessage(ctx context.Context, data []byte) {
	t.logf("Handling message...")
	var base struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Method  string          `json:"method"`
	}

	if err := json.Unmarshal(data, &base); err != nil {
		log.Printf("[eMCP stdio] ERROR: Failed to parse message: %v, data: %s", err, string(data))
		t.sendError(nil, emcp.ErrCodeParse, "parse error: "+err.Error())
		return
	}

	t.logf("Routing method: %s (id=%v)", base.Method, base.ID)

	// Route based on method
	switch base.Method {
	case "initialize":
		t.handleInitialize(ctx, base.ID, data)
	case "tools/list":
		t.handleToolsList(ctx, base.ID)
	case "tools/call":
		t.handleToolsCall(ctx, base.ID, data)
	case "ping":
		t.handlePing(ctx, base.ID)
	default:
		if strings.HasPrefix(base.Method, "notifications/") || base.ID == nil {
			t.logf("Ignoring notification: %s", base.Method)
		} else {
			log.Printf("[eMCP stdio] ERROR: Unknown method: %s", base.Method)
			t.sendError(base.ID, emcp.ErrCodeMethodNotFound, "method not found: "+base.Method)
		}
	}
}

// handleInitialize handles initialization request
func (t *StdioTransport) handleInitialize(ctx context.Context, id any, data []byte) {
	t.logf("Handling initialize request...")
	var req emcp.InitializeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("[eMCP stdio] ERROR: Failed to parse initialize request: %v", err)
		t.sendError(id, emcp.ErrCodeInvalidParams, "invalid params: "+err.Error())
		return
	}

	t.logf("Client info: %s v%s", req.Params.ClientInfo.Name, req.Params.ClientInfo.Version)

	info, err := t.server.Initialize(ctx, req.Params.ClientInfo)
	if err != nil {
		log.Printf("[eMCP stdio] ERROR: Server initialization failed: %v", err)
		t.sendError(id, emcp.ErrCodeInternal, "initialization failed: "+err.Error())
		return
	}

	t.logf("Server initialized successfully: %s v%s", info.Name, info.Version)

	resp := emcp.InitializeResult{
		ProtocolVersion: info.ProtocolVersion,
		ServerInfo:      *info,
		Capabilities:    info.Capabilities,
		Instructions:    t.server.instructions,
	}

	t.sendResult(id, resp)
	t.logf("Initialize response sent")
}

// handleToolsList handles tools/list request
func (t *StdioTransport) handleToolsList(ctx context.Context, id any) {
	tools, err := t.server.ListTools(ctx)
	if err != nil {
		t.sendError(id, emcp.ErrCodeInternal, "failed to list tools: "+err.Error())
		return
	}

	resp := struct {
		Tools []emcp.ToolDefinition `json:"tools"`
	}{
		Tools: tools,
	}

	t.sendResult(id, resp)
}

// handleToolsCall handles tools/call request
func (t *StdioTransport) handleToolsCall(ctx context.Context, id any, data []byte) {
	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string          `json:"name"`
			Arguments any    `json:"arguments"` // FIX: accept JSON objects
		} `json:"params"`
	}

	if err := json.Unmarshal(data, &req); err != nil {
		t.sendError(id, emcp.ErrCodeInvalidParams, "invalid params: "+err.Error())
		return
	}

	// Marshal arguments to []byte for tool handler
	var argsBytes []byte
	if req.Params.Arguments != nil {
		var err error
		argsBytes, err = json.Marshal(req.Params.Arguments)
		if err != nil {
			t.sendError(id, emcp.ErrCodeInvalidParams, "failed to marshal arguments: "+err.Error())
			return
		}
	}

	result, err := t.server.CallTool(ctx, req.Params.Name, argsBytes)
	if err != nil {
		t.sendError(id, emcp.ErrCodeInternal, "tool execution failed: "+err.Error())
		return
	}

	// Tool handler already returns complete CallToolResult JSON.
	// Pass through as raw JSON to avoid double-wrapping.
	t.sendResult(id, jsontext.Value(result))
}

// handlePing handles ping request
func (t *StdioTransport) handlePing(ctx context.Context, id any) {
	t.sendResult(id, map[string]any{})
}

// sendResult sends a JSON-RPC success response
func (t *StdioTransport) sendResult(id any, result any) {
	resp := emcp.JSONRPCResponse{
		JSONRPC: emcp.JSONRPCVersion,
		ID:      id,
		Result:  result,
	}

	t.send(resp)
}

// sendError sends a JSON-RPC error response
func (t *StdioTransport) sendError(id any, code int, message string) {
	resp := emcp.JSONRPCErrorResponse{
		JSONRPC: emcp.JSONRPCVersion,
		ID:      id,
		Error: emcp.ErrorDetail{
			Code:    code,
			Message: message,
		},
	}

	t.send(resp)
}

// send writes a message to stdout
func (t *StdioTransport) send(msg any) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		// CRITICAL: Log marshal errors - they should never be silent!
		log.Printf("[eMCP stdio] CRITICAL: Failed to marshal response: %v, message type: %T", err, msg)
		return
	}

	t.logf("Sending response: %s", string(data))

	if _, err := t.writer.Write(data); err != nil {
		log.Printf("[eMCP stdio] ERROR: Failed to write to stdout: %v", err)
		return
	}
	if err := t.writer.WriteByte('\n'); err != nil {
		log.Printf("[eMCP stdio] ERROR: Failed to write newline: %v", err)
		return
	}
	if err := t.writer.Flush(); err != nil {
		log.Printf("[eMCP stdio] ERROR: Failed to flush stdout: %v", err)
		return
	}

	t.logf("Response sent successfully")
}

// Shutdown gracefully shuts down the transport
func (t *StdioTransport) Shutdown(ctx context.Context) error {
	t.cancel()

	// Wait for all handlers to complete with timeout
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return t.server.Shutdown(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}