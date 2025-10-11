package server

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/goco-ai/emcp-go/emcp"
	emcpv1 "github.com/goco-ai/emcp-go/proto/emcp/v1"
)

// GRPCTransport implements gRPC transport for eMCP.
// Provides high-performance, bidirectional streaming, and production-ready features.
type GRPCTransport struct {
	server     *Server
	grpcServer *grpc.Server
	listener   net.Listener
	address    string
	verbose    bool

	// TLS configuration (optional)
	useTLS      bool
	credentials credentials.TransportCredentials

	// Graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Server implementation
	emcpv1.UnimplementedEMCPServiceServer
}

// GRPCTransportOption configures GRPC transport.
type GRPCTransportOption func(*GRPCTransport)

// WithTLS enables TLS for gRPC server.
func WithTLS(creds credentials.TransportCredentials) GRPCTransportOption {
	return func(t *GRPCTransport) {
		t.useTLS = true
		t.credentials = creds
	}
}

// WithVerbose enables verbose logging for gRPC transport.
func WithVerbose(verbose bool) GRPCTransportOption {
	return func(t *GRPCTransport) {
		t.verbose = verbose
	}
}

// NewGRPCTransport creates new gRPC transport for eMCP server.
//
// Example:
//
//	srv := server.New("my-server", "1.0.0")
//	transport := server.NewGRPCTransport(srv, "localhost:50051")
//	if err := transport.Serve(); err != nil {
//	    log.Fatal(err)
//	}
func NewGRPCTransport(server *Server, address string, opts ...GRPCTransportOption) *GRPCTransport {
	ctx, cancel := context.WithCancel(context.Background())

	t := &GRPCTransport{
		server:  server,
		address: address,
		ctx:     ctx,
		cancel:  cancel,
		verbose: false,
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	return t
}

// Serve starts the gRPC server and blocks until shutdown.
func (t *GRPCTransport) Serve() error {
	// Create listener
	listener, err := net.Listen("tcp", t.address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	t.listener = listener

	t.logf("gRPC server listening on %s", t.address)

	// Create gRPC server with options
	var serverOpts []grpc.ServerOption

	if t.useTLS && t.credentials != nil {
		serverOpts = append(serverOpts, grpc.Creds(t.credentials))
		t.logf("TLS enabled")
	}

	// Add interceptors for logging and error handling
	serverOpts = append(serverOpts,
		grpc.UnaryInterceptor(t.unaryInterceptor),
		grpc.StreamInterceptor(t.streamInterceptor),
	)

	t.grpcServer = grpc.NewServer(serverOpts...)

	// Register eMCP service
	emcpv1.RegisterEMCPServiceServer(t.grpcServer, t)

	t.logf("eMCP gRPC service registered")

	// Start serving
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := t.grpcServer.Serve(listener); err != nil {
			t.logf("gRPC server stopped: %v", err)
		}
	}()

	t.logf("eMCP gRPC server started successfully")

	// Wait for shutdown
	<-t.ctx.Done()
	return nil
}

// Shutdown gracefully stops the gRPC server.
func (t *GRPCTransport) Shutdown(ctx context.Context) error {
	t.logf("Shutting down gRPC server...")

	// Cancel context
	t.cancel()

	// Graceful stop with timeout
	done := make(chan struct{})
	go func() {
		t.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		t.logf("gRPC server stopped gracefully")
	case <-ctx.Done():
		t.logf("Forcing gRPC server stop...")
		t.grpcServer.Stop()
	}

	t.wg.Wait()
	return nil
}

// SetVerbose enables/disables verbose logging.
func (t *GRPCTransport) SetVerbose(verbose bool) {
	t.verbose = verbose
}

// logf logs message if verbose mode enabled.
func (t *GRPCTransport) logf(format string, args ...interface{}) {
	if t.verbose {
		log.Printf("[eMCP gRPC] "+format, args...)
	}
}

// =============================================================================
// gRPC Interceptors
// =============================================================================

func (t *GRPCTransport) unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()

	t.logf("→ %s", info.FullMethod)

	// Call handler
	resp, err := handler(ctx, req)

	// Log result
	duration := time.Since(start)
	if err != nil {
		t.logf("← %s failed: %v (took %v)", info.FullMethod, err, duration)
	} else {
		t.logf("← %s succeeded (took %v)", info.FullMethod, duration)
	}

	return resp, err
}

func (t *GRPCTransport) streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	t.logf("↔ %s (streaming)", info.FullMethod)
	return handler(srv, ss)
}

// =============================================================================
// eMCP Service Implementation
// =============================================================================

// Initialize implements MCP initialization handshake.
func (t *GRPCTransport) Initialize(
	ctx context.Context,
	req *emcpv1.InitializeRequest,
) (*emcpv1.InitializeResponse, error) {
	t.logf("Client initializing: %s v%s", req.ClientInfo.Name, req.ClientInfo.Version)

	// Get server info and capabilities
	clientInfo := emcp.ClientInfo{
		Name:    req.ClientInfo.Name,
		Version: req.ClientInfo.Version,
	}

	info, err := t.server.Initialize(ctx, clientInfo)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "initialization failed: %v", err)
	}

	// Convert to proto format
	resp := &emcpv1.InitializeResponse{
		ProtocolVersion: emcp.ProtocolVersion,
		ServerInfo: &emcpv1.ServerInfo{
			Name:            info.Name,
			Version:         info.Version,
			ProtocolVersion: info.ProtocolVersion,
			EmcpVersion:     emcp.EMCPVersion,
		},
		Capabilities: toProtoCapabilities(&info.Capabilities),
	}

	return resp, nil
}

// Ping implements health check.
func (t *GRPCTransport) Ping(ctx context.Context, req *emcpv1.PingRequest) (*emcpv1.PingResponse, error) {
	return &emcpv1.PingResponse{}, nil
}

// ListTools returns available tools.
func (t *GRPCTransport) ListTools(
	ctx context.Context,
	req *emcpv1.ListToolsRequest,
) (*emcpv1.ListToolsResponse, error) {
	tools, err := t.server.ListTools(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list tools: %v", err)
	}

	protoTools := make([]*emcpv1.ToolDefinition, len(tools))
	for i, tool := range tools {
		protoTools[i] = toProtoToolDefinition(&tool)
	}

	return &emcpv1.ListToolsResponse{
		Tools: protoTools,
	}, nil
}

// CallTool executes a tool.
func (t *GRPCTransport) CallTool(
	ctx context.Context,
	req *emcpv1.CallToolRequest,
) (*emcpv1.CallToolResponse, error) {
	start := time.Now()

	t.logf("Calling tool: %s", req.Name)

	// Convert arguments from Struct to JSON
	var args map[string]interface{}
	if req.Arguments != nil {
		args = req.Arguments.AsMap()
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid arguments: %v", err)
	}

	// Call tool
	resultBytes, err := t.server.CallTool(ctx, req.Name, argsJSON)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "tool execution failed: %v", err)
	}

	// Parse result as eMCP CallToolResult
	var result emcp.CallToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		// If not a structured result, treat as simple text content
		result = emcp.CallToolResult{
			Content: []emcp.ContentItem{
				{Type: "text", Text: string(resultBytes)},
			},
		}
	}

	// Convert result
	resp := &emcpv1.CallToolResponse{
		Content:       toProtoContent(result.Content),
		IsError:       result.IsError,
		ExecutionTime: durationpb.New(time.Since(start)),
	}

	t.logf("Tool %s completed in %v", req.Name, time.Since(start))

	return resp, nil
}

// ListResources returns available resources.
func (t *GRPCTransport) ListResources(
	ctx context.Context,
	req *emcpv1.ListResourcesRequest,
) (*emcpv1.ListResourcesResponse, error) {
	// TODO: Implement resources support
	return &emcpv1.ListResourcesResponse{
		Resources: []*emcpv1.ResourceDefinition{},
	}, nil
}

// ReadResource reads a resource.
func (t *GRPCTransport) ReadResource(
	ctx context.Context,
	req *emcpv1.ReadResourceRequest,
) (*emcpv1.ReadResourceResponse, error) {
	// TODO: Implement resource reading
	return nil, status.Error(codes.Unimplemented, "resources not yet supported")
}

// ListPrompts returns available prompts.
func (t *GRPCTransport) ListPrompts(
	ctx context.Context,
	req *emcpv1.ListPromptsRequest,
) (*emcpv1.ListPromptsResponse, error) {
	// TODO: Implement prompts support
	return &emcpv1.ListPromptsResponse{
		Prompts: []*emcpv1.PromptDefinition{},
	}, nil
}

// GetPrompt retrieves a prompt.
func (t *GRPCTransport) GetPrompt(
	ctx context.Context,
	req *emcpv1.GetPromptRequest,
) (*emcpv1.GetPromptResponse, error) {
	// TODO: Implement prompt retrieval
	return nil, status.Error(codes.Unimplemented, "prompts not yet supported")
}

// SetLogLevel sets logging level.
func (t *GRPCTransport) SetLogLevel(
	ctx context.Context,
	req *emcpv1.SetLogLevelRequest,
) (*emcpv1.SetLogLevelResponse, error) {
	// TODO: Implement log level management
	return &emcpv1.SetLogLevelResponse{}, nil
}

// Subscribe handles bidirectional streaming notifications.
func (t *GRPCTransport) Subscribe(stream emcpv1.EMCPService_SubscribeServer) error {
	t.logf("Client subscribed to notifications")

	// Handle streaming in both directions
	for {
		_, err := stream.Recv()
		if err != nil {
			return err
		}

		// TODO: Handle notifications
	}
}

// CreateCheckpoint creates a new checkpoint (eMCP extension).
func (t *GRPCTransport) CreateCheckpoint(
	ctx context.Context,
	req *emcpv1.CreateCheckpointRequest,
) (*emcpv1.CreateCheckpointResponse, error) {
	// TODO: Implement checkpoint creation
	return nil, status.Error(codes.Unimplemented, "checkpoints not yet supported")
}

// ListCheckpoints lists available checkpoints.
func (t *GRPCTransport) ListCheckpoints(
	ctx context.Context,
	req *emcpv1.ListCheckpointsRequest,
) (*emcpv1.ListCheckpointsResponse, error) {
	// TODO: Implement checkpoint listing
	return nil, status.Error(codes.Unimplemented, "checkpoints not yet supported")
}

// RestoreCheckpoint restores from a checkpoint.
func (t *GRPCTransport) RestoreCheckpoint(
	ctx context.Context,
	req *emcpv1.RestoreCheckpointRequest,
) (*emcpv1.RestoreCheckpointResponse, error) {
	// TODO: Implement checkpoint restoration
	return nil, status.Error(codes.Unimplemented, "checkpoints not yet supported")
}

// DeleteCheckpoint deletes a checkpoint.
func (t *GRPCTransport) DeleteCheckpoint(
	ctx context.Context,
	req *emcpv1.DeleteCheckpointRequest,
) (*emcpv1.DeleteCheckpointResponse, error) {
	// TODO: Implement checkpoint deletion
	return nil, status.Error(codes.Unimplemented, "checkpoints not yet supported")
}

// GetCheckpointInfo retrieves checkpoint information.
func (t *GRPCTransport) GetCheckpointInfo(
	ctx context.Context,
	req *emcpv1.GetCheckpointInfoRequest,
) (*emcpv1.GetCheckpointInfoResponse, error) {
	// TODO: Implement checkpoint info retrieval
	return nil, status.Error(codes.Unimplemented, "checkpoints not yet supported")
}

// CreateSession creates a new session.
func (t *GRPCTransport) CreateSession(
	ctx context.Context,
	req *emcpv1.CreateSessionRequest,
) (*emcpv1.CreateSessionResponse, error) {
	// TODO: Implement session creation
	return nil, status.Error(codes.Unimplemented, "sessions not yet supported")
}

// GetSession retrieves session information.
func (t *GRPCTransport) GetSession(
	ctx context.Context,
	req *emcpv1.GetSessionRequest,
) (*emcpv1.GetSessionResponse, error) {
	// TODO: Implement session retrieval
	return nil, status.Error(codes.Unimplemented, "sessions not yet supported")
}

// CloseSession closes a session.
func (t *GRPCTransport) CloseSession(
	ctx context.Context,
	req *emcpv1.CloseSessionRequest,
) (*emcpv1.CloseSessionResponse, error) {
	// TODO: Implement session closing
	return nil, status.Error(codes.Unimplemented, "sessions not yet supported")
}

// =============================================================================
// Conversion Helpers
// =============================================================================

func toProtoCapabilities(caps *emcp.Capabilities) *emcpv1.Capabilities {
	protoCaps := &emcpv1.Capabilities{}

	if caps.Tools != nil {
		protoCaps.Tools = &emcpv1.ToolsCapability{
			ListChanged: caps.Tools.ListChanged,
		}
	}

	if caps.Resources != nil {
		protoCaps.Resources = &emcpv1.ResourcesCapability{
			Subscribe:   caps.Resources.Subscribe,
			ListChanged: caps.Resources.ListChanged,
		}
	}

	if caps.Prompts != nil {
		protoCaps.Prompts = &emcpv1.PromptsCapability{
			ListChanged: caps.Prompts.ListChanged,
		}
	}

	if caps.Logging != nil {
		protoCaps.Logging = &emcpv1.LoggingCapability{}
	}

	// Convert experimental capabilities
	if caps.Experimental != nil {
		// TODO: Convert to structpb.Struct
	}

	return protoCaps
}

func toProtoToolDefinition(tool *emcp.ToolDefinition) *emcpv1.ToolDefinition {
	return &emcpv1.ToolDefinition{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: toProtoJSONSchema(tool.InputSchema),
		RiskLevel:   toProtoRiskLevel(tool.RiskLevel),
		RequiresCheckpoint: tool.RequiresCheckpoint,
	}
}

func toProtoJSONSchema(schema *emcp.JSONSchema) *emcpv1.JSONSchema {
	if schema == nil {
		return nil
	}

	protoSchema := &emcpv1.JSONSchema{
		Type:     schema.Type,
		Required: schema.Required,
	}

	if schema.Properties != nil {
		protoSchema.Properties = make(map[string]*emcpv1.SchemaProperty)
		for name, prop := range schema.Properties {
			protoSchema.Properties[name] = &emcpv1.SchemaProperty{
				Type:        prop.Type,
				Description: prop.Description,
				Enum:        prop.Enum,
				Format:      prop.Format,
			}
		}
	}

	return protoSchema
}

func toProtoRiskLevel(level emcp.RiskLevel) emcpv1.RiskLevel {
	switch level {
	case emcp.RiskLow:
		return emcpv1.RiskLevel_RISK_LEVEL_LOW
	case emcp.RiskMedium:
		return emcpv1.RiskLevel_RISK_LEVEL_MEDIUM
	case emcp.RiskHigh:
		return emcpv1.RiskLevel_RISK_LEVEL_HIGH
	case emcp.RiskCritical:
		return emcpv1.RiskLevel_RISK_LEVEL_CRITICAL
	default:
		return emcpv1.RiskLevel_RISK_LEVEL_UNKNOWN
	}
}

func toProtoContent(content []emcp.ContentItem) []*emcpv1.ContentItem {
	protoContent := make([]*emcpv1.ContentItem, len(content))
	for i, item := range content {
		protoContent[i] = &emcpv1.ContentItem{
			Type:     item.Type,
			Text:     item.Text,
			Data:     item.Data,
			MimeType: item.MimeType,
			Uri:      item.URI,
		}
	}
	return protoContent
}
