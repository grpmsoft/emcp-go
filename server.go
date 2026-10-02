// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	emcpgrpc "github.com/grpmsoft/emcp-go/grpc"
	"github.com/grpmsoft/emcp-go/internal/mcpserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ToolHandler is the handler function for an MCP tool. It receives the tool
// name, decoded arguments as a map, and returns a ToolResult or error.
//
// Returning a non-nil error is treated as a protocol-level error (the call
// fails). To report an application-level error, return a ToolResult with
// IsError=true and a text content item describing the error.
type ToolHandler func(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

// ServerConfig holds parameters for creating a Server.
type ServerConfig struct {
	// Name identifies the server to connected clients (sent in MCP initialize).
	Name string
	// Version is the server version string (sent in MCP initialize).
	Version string
	// Instructions are optional instructions sent to clients via MCP initialize.
	Instructions string

	// Stateful enables stateful HTTP mode with session tracking.
	// By default (Stateful=false), the server runs in stateless mode per the
	// MCP 2026-07-28 spec: no Mcp-Session-Id header, each request gets a
	// temporary session. Set Stateful=true to enable session persistence
	// across requests.
	Stateful bool

	// SessionTimeout configures how long idle sessions survive before
	// automatic cleanup. Zero means never expire. Only relevant when
	// Stateful is true.
	SessionTimeout time.Duration

	// TokenValidator, when non-nil, enables server-side bearer token
	// verification. For HTTP, incoming requests must carry an
	// "Authorization: Bearer <token>" header; for gRPC, the "authorization"
	// metadata key must be set. The validator is called with the raw token
	// (without the "Bearer " prefix). If it returns false, the request is
	// rejected with 401 Unauthorized (HTTP) or codes.Unauthenticated (gRPC).
	//
	// When nil (the default), no authentication is enforced and all
	// requests are accepted. This preserves backwards compatibility.
	TokenValidator func(token string) bool
}

// Server is a unified MCP server that supports HTTP and gRPC transports.
// It delegates all work to the internal mcpserver implementation, keeping
// the official SDK types out of the public API.
//
// Usage:
//
//	srv := emcp.NewServer(emcp.ServerConfig{Name: "my-server", Version: "1.0.0"})
//	srv.AddTool("echo", "echoes the input", nil, func(ctx context.Context, name string, args map[string]any) (*emcp.ToolResult, error) {
//	    msg, _ := args["msg"].(string)
//	    return emcp.TextResult("echo: " + msg), nil
//	})
//
//	mux := http.NewServeMux()
//	mux.Handle("/mcp", srv.HTTPHandler())
//	http.ListenAndServe(":8080", mux)
type Server struct {
	impl           *mcpserver.Server
	tokenValidator func(token string) bool
}

// NewServer creates a new MCP server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		impl: mcpserver.New(mcpserver.Config{
			Name:           cfg.Name,
			Version:        cfg.Version,
			Instructions:   cfg.Instructions,
			Stateless:      !cfg.Stateful,
			SessionTimeout: cfg.SessionTimeout,
			TokenValidator: cfg.TokenValidator,
		}),
		tokenValidator: cfg.TokenValidator,
	}
}

// AddTool registers a tool with its handler. The inputSchema is a JSON Schema
// object (typically map[string]any) describing the tool's expected parameters.
// Pass nil for tools that accept no arguments.
func (s *Server) AddTool(name, description string, inputSchema map[string]any, handler ToolHandler) {
	s.impl.AddTool(name, description, inputSchema, func(ctx context.Context, n string, args map[string]any) (*mcpserver.ToolResult, error) {
		result, err := handler(ctx, n, args)
		if err != nil {
			return nil, err
		}
		return toInternalResult(result), nil
	})
}

// HTTPHandler returns an http.Handler for serving MCP over Streamable HTTP.
// Mount this on your HTTP mux: mux.Handle("/mcp", srv.HTTPHandler())
//
// When ServerConfig.TokenValidator is set, the returned handler rejects
// requests that do not carry a valid "Authorization: Bearer <token>" header
// with 401 Unauthorized before they reach the MCP layer.
func (s *Server) HTTPHandler() http.Handler {
	h := s.impl.HTTPHandler()
	if s.tokenValidator == nil {
		return h
	}
	return &bearerAuthMiddleware{
		next:     h,
		validate: s.tokenValidator,
	}
}

// GRPCHandler returns the gRPC handler for registering with a grpc.Server.
// Use: emcpv1.RegisterMCPTransportServer(grpcServer, srv.GRPCHandler())
//
// When ServerConfig.TokenValidator is set, callers should register the
// stream interceptor returned by GRPCAuthInterceptor() on the grpc.Server
// to enforce bearer token verification on incoming streams.
func (s *Server) GRPCHandler() *emcpgrpc.GRPCHandler {
	return s.impl.GRPCHandler()
}

// GRPCAuthInterceptor returns a grpc.StreamServerInterceptor that verifies
// bearer tokens from the "authorization" metadata key. Returns nil when no
// TokenValidator is configured (no auth required).
//
// Usage:
//
//	interceptor := srv.GRPCAuthInterceptor()
//	opts := []grpc.ServerOption{}
//	if interceptor != nil {
//	    opts = append(opts, grpc.StreamInterceptor(interceptor))
//	}
//	gs := grpc.NewServer(opts...)
func (s *Server) GRPCAuthInterceptor() grpc.StreamServerInterceptor {
	if s.tokenValidator == nil {
		return nil
	}
	validate := s.tokenValidator
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		vals := md.Get("authorization")
		if len(vals) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization metadata")
		}
		token := vals[0]
		const prefix = "Bearer "
		if !strings.HasPrefix(token, prefix) {
			return status.Error(codes.Unauthenticated, "authorization must use Bearer scheme")
		}
		if !validate(strings.TrimPrefix(token, prefix)) {
			return status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(srv, ss)
	}
}

// Internal returns the underlying MCP server for advanced use cases.
// This is an escape hatch -- prefer the public API.
func (s *Server) Internal() any {
	return s.impl.MCPServer()
}

// Close shuts down the server and releases all resources.
func (s *Server) Close() error {
	// The SDK's mcp.Server does not have a Close method; tools and handlers
	// are garbage collected. This method exists for API symmetry with Client
	// and for future resource cleanup.
	return nil
}

// TextResult is a convenience constructor for a ToolResult with a single text
// content item. This is the most common result type.
func TextResult(text string) *ToolResult {
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: text}},
	}
}

// ErrorResult is a convenience constructor for a ToolResult that represents
// an application-level error. The error message is returned as text content
// with IsError=true.
func ErrorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: msg}},
	}
}

// StaticToken returns a TokenValidator function that compares the incoming
// token against a fixed expected value using constant-time comparison.
// This prevents timing side-channel attacks on the token.
//
// Usage:
//
//	srv := emcp.NewServer(emcp.ServerConfig{
//	    TokenValidator: emcp.StaticToken("my-secret-token"),
//	})
func StaticToken(tok string) func(string) bool {
	expected := []byte(tok)
	return func(candidate string) bool {
		return subtle.ConstantTimeCompare(expected, []byte(candidate)) == 1
	}
}

// bearerAuthMiddleware is an http.Handler that extracts and validates a
// Bearer token from the Authorization header before delegating to the next
// handler. Invalid or missing tokens are rejected with 401 Unauthorized.
type bearerAuthMiddleware struct {
	next     http.Handler
	validate func(token string) bool
}

func (m *bearerAuthMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(auth, prefix)
	if !m.validate(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	m.next.ServeHTTP(w, r)
}

// toInternalResult converts a public ToolResult to the internal representation.
// B7 fix: passes through all content types (resource_link, embedded resource)
// and StructuredContent to the internal layer for proper SDK conversion.
func toInternalResult(r *ToolResult) *mcpserver.ToolResult {
	if r == nil {
		return &mcpserver.ToolResult{}
	}
	result := &mcpserver.ToolResult{
		IsError:           r.IsError,
		StructuredContent: r.StructuredContent,
	}
	for _, item := range r.Content {
		ci := mcpserver.ContentItem{
			Type:        item.Type,
			Text:        item.Text,
			MIMEType:    item.MIMEType,
			Data:        item.Data,
			URI:         item.URI,
			Name:        item.Name,
			Title:       item.Title,
			Description: item.Description,
		}
		if item.Resource != nil {
			ci.Resource = &mcpserver.ResourceContents{
				URI:      item.Resource.URI,
				MIMEType: item.Resource.MIMEType,
				Text:     item.Resource.Text,
				Blob:     item.Resource.Blob,
			}
		}
		result.Content = append(result.Content, ci)
	}
	return result
}
