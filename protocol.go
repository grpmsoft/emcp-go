package emcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Protocol implements MCP 1.0 JSON-RPC protocol
type Protocol struct {
	mu sync.RWMutex

	// Server info
	serverInfo ServerInfo

	// Registered tools, resources, prompts
	tools     map[string]*ToolDefinition
	resources map[string]*ResourceDefinition
	prompts   map[string]*PromptDefinition

	// Type-safe tool handlers (generics)
	toolHandlers map[string]interface{} // map[string]func(ctx, TInput) (TOutput, error)

	// Middleware chain
	middleware []Middleware

	// Lifecycle hooks
	onInitialize    func(context.Context, *InitializeRequest) error
	onShutdown      func(context.Context) error
	onToolExecute   func(context.Context, string, json.RawMessage) error

	// State
	initialized bool
	clientInfo  *ClientInfo
}

// ProtocolConfig configures the MCP protocol
type ProtocolConfig struct {
	ServerInfo ServerInfo
	Middleware []Middleware

	// Lifecycle hooks
	OnInitialize  func(context.Context, *InitializeRequest) error
	OnShutdown    func(context.Context) error
	OnToolExecute func(context.Context, string, json.RawMessage) error
}

// NewProtocol creates a new MCP protocol handler
func NewProtocol(cfg ProtocolConfig) *Protocol {
	if cfg.ServerInfo.Name == "" {
		cfg.ServerInfo.Name = "emcp-server"
	}
	if cfg.ServerInfo.Version == "" {
		cfg.ServerInfo.Version = "0.1.0"
	}
	if cfg.ServerInfo.ProtocolVersion == "" {
		cfg.ServerInfo.ProtocolVersion = "1.0"
	}

	return &Protocol{
		serverInfo:      cfg.ServerInfo,
		tools:           make(map[string]*ToolDefinition),
		resources:       make(map[string]*ResourceDefinition),
		prompts:         make(map[string]*PromptDefinition),
		toolHandlers:    make(map[string]interface{}),
		middleware:      cfg.Middleware,
		onInitialize:    cfg.OnInitialize,
		onShutdown:      cfg.OnShutdown,
		onToolExecute:   cfg.OnToolExecute,
	}
}

// RegisterTool registers a type-safe tool with compile-time checking
func RegisterTool[TInput, TOutput any](p *Protocol, tool Tool[TInput, TOutput]) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.tools[tool.Name]; exists {
		return fmt.Errorf("tool %s already registered", tool.Name)
	}

	// Store type-erased definition for MCP 1.0 wire format
	p.tools[tool.Name] = &ToolDefinition{
		Name:               tool.Name,
		Description:        tool.Description,
		InputSchema:        tool.InputSchema,
		RiskLevel:          tool.RiskLevel,
		RequiresCheckpoint: tool.RequiresCheckpoint,
		Metadata:           tool.Metadata,
	}

	// Store typed handler for runtime execution
	p.toolHandlers[tool.Name] = tool.Handler

	return nil
}

// RegisterResource registers a resource
func (p *Protocol) RegisterResource(resource ResourceDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.resources[resource.URI]; exists {
		return fmt.Errorf("resource %s already registered", resource.URI)
	}

	p.resources[resource.URI] = &resource
	return nil
}

// RegisterPrompt registers a prompt
func (p *Protocol) RegisterPrompt(prompt PromptDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.prompts[prompt.Name]; exists {
		return fmt.Errorf("prompt %s already registered", prompt.Name)
	}

	p.prompts[prompt.Name] = &prompt
	return nil
}

// HandleRequest handles incoming JSON-RPC request
func (p *Protocol) HandleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// Apply middleware chain
	ctx = p.applyMiddleware(ctx, req)

	// Route to appropriate handler
	switch req.Method {
	case "initialize":
		return p.handleInitialize(ctx, req)
	case "shutdown":
		return p.handleShutdown(ctx, req)
	case "tools/list":
		return p.handleToolsList(ctx, req)
	case "tools/call":
		return p.handleToolsCall(ctx, req)
	case "resources/list":
		return p.handleResourcesList(ctx, req)
	case "resources/read":
		return p.handleResourcesRead(ctx, req)
	case "prompts/list":
		return p.handlePromptsList(ctx, req)
	case "prompts/get":
		return p.handlePromptsGet(ctx, req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    ErrCodeMethodNotFound,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

// applyMiddleware applies middleware chain to context
func (p *Protocol) applyMiddleware(ctx context.Context, req *JSONRPCRequest) context.Context {
	for _, mw := range p.middleware {
		ctx = mw.Process(ctx, req)
	}
	return ctx
}

// handleInitialize handles initialize request
func (p *Protocol) handleInitialize(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var initReq InitializeRequest
	if err := json.Unmarshal(req.Params, &initReq); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid initialize params", err)
	}

	// Store client info
	p.mu.Lock()
	p.clientInfo = &initReq.ClientInfo
	p.initialized = true
	p.mu.Unlock()

	// Call lifecycle hook
	if p.onInitialize != nil {
		if err := p.onInitialize(ctx, &initReq); err != nil {
			return errorResponse(req.ID, ErrCodeInternal, "initialization failed", err)
		}
	}

	// Build capabilities
	capabilities := Capabilities{
		Tools:     map[string]interface{}{"listChanged": true},
		Resources: map[string]interface{}{"listChanged": true},
		Prompts:   map[string]interface{}{"listChanged": true},
	}

	// Add eMCP extensions if client supports them
	if supportsEMCP(initReq.ClientInfo) {
		capabilities.Experimental = map[string]interface{}{
			"emcp": map[string]interface{}{
				"checkpoints":  true,
				"riskAssessment": true,
				"multiAgent":   true,
			},
		}
	}

	result := InitializeResponse{
		ProtocolVersion: p.serverInfo.ProtocolVersion,
		ServerInfo:      p.serverInfo,
		Capabilities:    capabilities,
	}

	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleShutdown handles shutdown request
func (p *Protocol) handleShutdown(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	if p.onShutdown != nil {
		if err := p.onShutdown(ctx); err != nil {
			return errorResponse(req.ID, ErrCodeInternal, "shutdown failed", err)
		}
	}

	p.mu.Lock()
	p.initialized = false
	p.mu.Unlock()

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  json.RawMessage("{}"),
	}
}

// handleToolsList handles tools/list request
func (p *Protocol) handleToolsList(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tools := make([]*ToolDefinition, 0, len(p.tools))
	for _, tool := range p.tools {
		tools = append(tools, tool)
	}

	result := ToolsListResponse{Tools: tools}
	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsCall handles tools/call request
func (p *Protocol) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var callReq ToolCallRequest
	if err := json.Unmarshal(req.Params, &callReq); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidParams, "invalid tool call params", err)
	}

	// Get tool definition
	p.mu.RLock()
	toolDef, exists := p.tools[callReq.Name]
	handler, hasHandler := p.toolHandlers[callReq.Name]
	p.mu.RUnlock()

	if !exists || !hasHandler {
		return errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("tool not found: %s", callReq.Name), nil)
	}

	// Check risk level and checkpoint requirement
	if toolDef.RequiresCheckpoint {
		// eMCP extension: verify checkpoint exists
		if !hasCheckpoint(ctx) {
			return errorResponse(req.ID, ErrCodeCheckpointRequired,
				fmt.Sprintf("tool %s requires checkpoint before execution", callReq.Name), nil)
		}
	}

	// Execute lifecycle hook
	if p.onToolExecute != nil {
		if err := p.onToolExecute(ctx, callReq.Name, callReq.Arguments); err != nil {
			return errorResponse(req.ID, ErrCodeInternal, "tool execution hook failed", err)
		}
	}

	// Execute tool (type-safe handler)
	result, err := p.executeTool(ctx, handler, callReq.Arguments)
	if err != nil {
		return errorResponse(req.ID, ErrCodeInternal, fmt.Sprintf("tool execution failed: %s", callReq.Name), err)
	}

	resultJSON, _ := json.Marshal(ToolCallResponse{
		Content: result,
		IsError: false,
	})

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// executeTool executes a tool with type-safe handler
func (p *Protocol) executeTool(ctx context.Context, handler interface{}, args json.RawMessage) (interface{}, error) {
	// Type assertion to generic handler
	// This is type-safe at compile time when RegisterTool is used
	switch h := handler.(type) {
	case func(context.Context, map[string]interface{}) (interface{}, error):
		var input map[string]interface{}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}
		return h(ctx, input)
	default:
		// Fallback for custom types - unmarshal to interface{}
		var input interface{}
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &input); err != nil {
				return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
			}
		}

		// Use reflection for custom handlers
		// TODO: Add proper reflection-based invocation
		return nil, fmt.Errorf("custom handler types not yet implemented")
	}
}

// handleResourcesList handles resources/list request
func (p *Protocol) handleResourcesList(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	resources := make([]*ResourceDefinition, 0, len(p.resources))
	for _, resource := range p.resources {
		resources = append(resources, resource)
	}

	result := ResourcesListResponse{Resources: resources}
	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleResourcesRead handles resources/read request (stub)
func (p *Protocol) handleResourcesRead(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// TODO: Implement resource reading
	return errorResponse(req.ID, ErrCodeInternal, "resources/read not implemented yet", nil)
}

// handlePromptsList handles prompts/list request
func (p *Protocol) handlePromptsList(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prompts := make([]*PromptDefinition, 0, len(p.prompts))
	for _, prompt := range p.prompts {
		prompts = append(prompts, prompt)
	}

	result := PromptsListResponse{Prompts: prompts}
	resultJSON, _ := json.Marshal(result)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handlePromptsGet handles prompts/get request (stub)
func (p *Protocol) handlePromptsGet(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	// TODO: Implement prompt retrieval
	return errorResponse(req.ID, ErrCodeInternal, "prompts/get not implemented yet", nil)
}

// Helper functions

func errorResponse(id interface{}, code int, message string, err error) *JSONRPCResponse {
	errMsg := message
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", message, err)
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: errMsg,
		},
	}
}

func supportsEMCP(clientInfo ClientInfo) bool {
	// Check if client supports eMCP extensions
	if exp, ok := clientInfo.Capabilities.Experimental.(map[string]interface{}); ok {
		if _, hasEMCP := exp["emcp"]; hasEMCP {
			return true
		}
	}
	return false
}

func hasCheckpoint(ctx context.Context) bool {
	// Check if checkpoint exists in context (eMCP extension)
	if cp := ctx.Value("checkpoint"); cp != nil {
		return true
	}
	return false
}

// MCP 1.0 Protocol types

type InitializeRequest struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ClientInfo      ClientInfo   `json:"clientInfo"`
}

type InitializeResponse struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

type ClientInfo struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Capabilities Capabilities `json:"capabilities,omitempty"`
}

type ToolsListResponse struct {
	Tools []*ToolDefinition `json:"tools"`
}

type ToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolCallResponse struct {
	Content interface{} `json:"content"`
	IsError bool        `json:"isError,omitempty"`
}

type ResourcesListResponse struct {
	Resources []*ResourceDefinition `json:"resources"`
}

type PromptsListResponse struct {
	Prompts []*PromptDefinition `json:"prompts"`
}