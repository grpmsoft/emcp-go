// Deprecated: Package server is part of eMCP v0.1 and will be removed.
// Use github.com/modelcontextprotocol/go-sdk/mcp for MCP server functionality,
// and github.com/goco-ai/emcp-go/middleware for enterprise middleware.
package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/goco-ai/emcp-go/emcp"
)

// Server implements an Enhanced MCP server with enterprise features.
type Server struct {
	// Configuration
	info         emcp.ServerInfo
	instructions string // MCP instructions injected into LLM system prompt

	// Resource management
	toolsMu   sync.RWMutex
	tools     map[string]*toolEntry

	// Middleware
	middlewares []ToolMiddleware

	// Hooks
	initHook    InitHook
	shutdownHook ShutdownHook
}

// toolEntry holds a tool and its handler
type toolEntry struct {
	definition emcp.ToolDefinition
	handler    ToolHandler
}

// ToolHandler handles tool execution
type ToolHandler func(ctx context.Context, params []byte) ([]byte, error)

// ToolMiddleware wraps a ToolHandler for cross-cutting concerns
type ToolMiddleware func(ToolHandler) ToolHandler

// InitHook is called during server initialization
type InitHook func(ctx context.Context, clientInfo emcp.ClientInfo) error

// ShutdownHook is called during server shutdown
type ShutdownHook func(ctx context.Context) error

// Option configures a Server
type Option func(*Server)

// WithInstructions sets MCP instructions injected into LLM system prompt.
// Use to guide agent behavior: which tools to prefer, when to fall back.
func WithInstructions(instructions string) Option {
	return func(s *Server) { s.instructions = instructions }
}

// GetInstructions returns the MCP instructions string.
func (s *Server) GetInstructions() string {
	return s.instructions
}

// New creates a new eMCP server with the given options
func New(name, version string, opts ...Option) *Server {
	s := &Server{
		info: emcp.ServerInfo{
			Name:            name,
			Version:         version,
			ProtocolVersion: emcp.ProtocolVersion,
			Capabilities: emcp.ServerCapabilities{
				Tools: &emcp.ToolsCapability{},
			},
		},
		tools: make(map[string]*toolEntry),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// WithMiddleware adds middleware to the server
func WithMiddleware(mw ToolMiddleware) Option {
	return func(s *Server) {
		s.middlewares = append(s.middlewares, mw)
	}
}

// WithInitHook sets the initialization hook
func WithInitHook(hook InitHook) Option {
	return func(s *Server) {
		s.initHook = hook
	}
}

// WithShutdownHook sets the shutdown hook
func WithShutdownHook(hook ShutdownHook) Option {
	return func(s *Server) {
		s.shutdownHook = hook
	}
}

// AddTool registers a new tool with the server
func (s *Server) AddTool(def emcp.ToolDefinition, handler ToolHandler) error {
	if def.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if def.InputSchema == nil {
		return fmt.Errorf("tool %s: input schema cannot be nil", def.Name)
	}
	if handler == nil {
		return fmt.Errorf("tool %s: handler cannot be nil", def.Name)
	}

	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()

	if _, exists := s.tools[def.Name]; exists {
		return fmt.Errorf("tool %s already registered", def.Name)
	}

	// Wrap handler with middlewares
	wrapped := handler
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		wrapped = s.middlewares[i](wrapped)
	}

	s.tools[def.Name] = &toolEntry{
		definition: def,
		handler:    wrapped,
	}

	return nil
}

// ListTools returns all registered tools
func (s *Server) ListTools(ctx context.Context) ([]emcp.ToolDefinition, error) {
	s.toolsMu.RLock()
	defer s.toolsMu.RUnlock()

	tools := make([]emcp.ToolDefinition, 0, len(s.tools))
	for _, entry := range s.tools {
		tools = append(tools, entry.definition)
	}

	return tools, nil
}

// CallTool executes a tool by name
func (s *Server) CallTool(ctx context.Context, name string, params []byte) ([]byte, error) {
	s.toolsMu.RLock()
	entry, exists := s.tools[name]
	s.toolsMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	return entry.handler(ctx, params)
}

// Initialize handles server initialization
func (s *Server) Initialize(ctx context.Context, clientInfo emcp.ClientInfo) (*emcp.ServerInfo, error) {
	if s.initHook != nil {
		if err := s.initHook(ctx, clientInfo); err != nil {
			return nil, fmt.Errorf("init hook failed: %w", err)
		}
	}

	return &s.info, nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.shutdownHook != nil {
		return s.shutdownHook(ctx)
	}
	return nil
}

// Info returns server information
func (s *Server) Info() emcp.ServerInfo {
	return s.info
}