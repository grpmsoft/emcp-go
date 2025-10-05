// Package server provides stdio transport for MCP protocol compatibility
package server

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/goco-ai/emcp-go/emcp"
)

// StdioTransport implements stdio-based JSON-RPC transport for MCP 1.0 compatibility
type StdioTransport struct {
	server *Server
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex

	// Graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(server *Server) *StdioTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &StdioTransport{
		server: server,
		reader: bufio.NewReader(os.Stdin),
		writer: bufio.NewWriter(os.Stdout),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Serve starts the stdio transport loop
func (t *StdioTransport) Serve() error {
	t.wg.Add(1)
	defer t.wg.Done()

	for {
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		default:
			// Read one line (JSON-RPC message)
			line, err := t.reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("read error: %w", err)
			}

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
	var base struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Method  string          `json:"method"`
	}

	if err := json.Unmarshal(data, &base); err != nil {
		t.sendError(nil, emcp.ErrCodeParse, "parse error: "+err.Error())
		return
	}

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
		t.sendError(base.ID, emcp.ErrCodeMethodNotFound, "method not found: "+base.Method)
	}
}

// handleInitialize handles initialization request
func (t *StdioTransport) handleInitialize(ctx context.Context, id any, data []byte) {
	var req emcp.InitializeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.sendError(id, emcp.ErrCodeInvalidParams, "invalid params: "+err.Error())
		return
	}

	info, err := t.server.Initialize(ctx, req.Params.ClientInfo)
	if err != nil {
		t.sendError(id, emcp.ErrCodeInternal, "initialization failed: "+err.Error())
		return
	}

	resp := emcp.InitializeResult{
		ProtocolVersion: info.ProtocolVersion,
		ServerInfo:      *info,
		Capabilities:    info.Capabilities,
	}

	t.sendResult(id, resp)
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
			Arguments []byte `json:"arguments"`
		} `json:"params"`
	}

	if err := json.Unmarshal(data, &req); err != nil {
		t.sendError(id, emcp.ErrCodeInvalidParams, "invalid params: "+err.Error())
		return
	}

	result, err := t.server.CallTool(ctx, req.Params.Name, req.Params.Arguments)
	if err != nil {
		t.sendError(id, emcp.ErrCodeInternal, "tool execution failed: "+err.Error())
		return
	}

	resp := struct {
		Content []struct {
			Type string          `json:"type"`
			Text []byte `json:"text"`
		} `json:"content"`
	}{
		Content: []struct {
			Type string          `json:"type"`
			Text []byte `json:"text"`
		}{
			{
				Type: "text",
				Text: result,
			},
		},
	}

	t.sendResult(id, resp)
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
		// Log error but don't fail
		return
	}

	t.writer.Write(data)
	t.writer.WriteByte('\n')
	t.writer.Flush()
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