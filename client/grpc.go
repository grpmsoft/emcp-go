package client

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/goco-ai/emcp-go/emcp"
	emcpv1 "github.com/goco-ai/emcp-go/proto/emcp/v1"
)

// GRPCTransport implements gRPC transport for eMCP client.
// Provides high-performance, bi-directional streaming capabilities.
type GRPCTransport struct {
	conn   *grpc.ClientConn
	client emcpv1.EMCPServiceClient
	target string

	// Configuration
	useTLS      bool
	credentials credentials.TransportCredentials
	opts        []grpc.DialOption

	// Context management
	ctx    context.Context
	cancel context.CancelFunc
}

// GRPCTransportOption configures gRPC transport.
type GRPCTransportOption func(*GRPCTransport)

// WithGRPCTLS enables TLS for gRPC connection.
func WithGRPCTLS(creds credentials.TransportCredentials) GRPCTransportOption {
	return func(t *GRPCTransport) {
		t.useTLS = true
		t.credentials = creds
	}
}

// WithGRPCDialOptions adds custom dial options.
func WithGRPCDialOptions(opts ...grpc.DialOption) GRPCTransportOption {
	return func(t *GRPCTransport) {
		t.opts = append(t.opts, opts...)
	}
}

// NewGRPCTransport creates a new gRPC transport for eMCP client.
//
// Example:
//
//	transport, err := client.NewGRPCTransport("localhost:50051")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer transport.Close()
//
//	c := client.New(transport)
//	c.Initialize(ctx)
func NewGRPCTransport(target string, opts ...GRPCTransportOption) (*GRPCTransport, error) {
	ctx, cancel := context.WithCancel(context.Background())

	t := &GRPCTransport{
		target: target,
		ctx:    ctx,
		cancel: cancel,
		opts:   []grpc.DialOption{},
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	// Add TLS or insecure credentials
	if t.useTLS && t.credentials != nil {
		t.opts = append(t.opts, grpc.WithTransportCredentials(t.credentials))
	} else {
		t.opts = append(t.opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Add default dial options
	t.opts = append(t.opts,
		grpc.WithBlock(),
		grpc.WithTimeout(30*time.Second),
	)

	// Establish connection
	conn, err := grpc.Dial(target, t.opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to %s: %w", target, err)
	}

	t.conn = conn
	t.client = emcpv1.NewEMCPServiceClient(conn)

	return t, nil
}

// Initialize performs eMCP handshake.
func (t *GRPCTransport) Initialize(ctx context.Context, clientInfo emcp.ClientInfo) (*emcp.ServerInfo, error) {
	req := &emcpv1.InitializeRequest{
		ProtocolVersion: emcp.ProtocolVersion,
		ClientInfo: &emcpv1.ClientInfo{
			Name:    clientInfo.Name,
			Version: clientInfo.Version,
		},
		Capabilities: &emcpv1.Capabilities{
			Tools: &emcpv1.ToolsCapability{},
		},
	}

	resp, err := t.client.Initialize(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	// Convert response
	info := &emcp.ServerInfo{
		Name:            resp.ServerInfo.Name,
		Version:         resp.ServerInfo.Version,
		ProtocolVersion: resp.ProtocolVersion,
		Capabilities:    fromProtoCapabilities(resp.Capabilities),
	}

	return info, nil
}

// ListTools lists available tools.
func (t *GRPCTransport) ListTools(ctx context.Context) ([]emcp.ToolDefinition, error) {
	req := &emcpv1.ListToolsRequest{}

	resp, err := t.client.ListTools(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list tools failed: %w", err)
	}

	tools := make([]emcp.ToolDefinition, len(resp.Tools))
	for i, protoTool := range resp.Tools {
		tools[i] = fromProtoToolDefinition(protoTool)
	}

	return tools, nil
}

// CallTool executes a tool.
func (t *GRPCTransport) CallTool(ctx context.Context, name string, arguments any) ([]byte, error) {
	// Convert arguments to Struct
	var protoArgs *structpb.Struct
	if arguments != nil {
		argsJSON, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal arguments: %w", err)
		}

		var argsMap map[string]interface{}
		if err := json.Unmarshal(argsJSON, &argsMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		protoArgs, err = structpb.NewStruct(argsMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert arguments: %w", err)
		}
	}

	req := &emcpv1.CallToolRequest{
		Name:      name,
		Arguments: protoArgs,
	}

	resp, err := t.client.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call tool failed: %w", err)
	}

	// Convert response to CallToolResult
	result := emcp.CallToolResult{
		Content: fromProtoContent(resp.Content),
		IsError: resp.IsError,
	}

	return json.Marshal(result)
}

// Ping checks server health.
func (t *GRPCTransport) Ping(ctx context.Context) error {
	req := &emcpv1.PingRequest{}
	_, err := t.client.Ping(ctx, req)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

// Close closes the gRPC connection.
func (t *GRPCTransport) Close() error {
	t.cancel()
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

func fromProtoCapabilities(caps *emcpv1.Capabilities) emcp.Capabilities {
	result := emcp.Capabilities{}

	if caps.Tools != nil {
		result.Tools = &emcp.ToolsCapability{
			ListChanged: caps.Tools.ListChanged,
		}
	}

	if caps.Resources != nil {
		result.Resources = &emcp.ResourcesCapability{
			Subscribe:   caps.Resources.Subscribe,
			ListChanged: caps.Resources.ListChanged,
		}
	}

	if caps.Prompts != nil {
		result.Prompts = &emcp.PromptsCapability{
			ListChanged: caps.Prompts.ListChanged,
		}
	}

	if caps.Logging != nil {
		result.Logging = &emcp.LoggingCapability{}
	}

	return result
}

func fromProtoToolDefinition(tool *emcpv1.ToolDefinition) emcp.ToolDefinition {
	return emcp.ToolDefinition{
		Name:               tool.Name,
		Description:        tool.Description,
		InputSchema:        fromProtoJSONSchema(tool.InputSchema),
		RiskLevel:          fromProtoRiskLevel(tool.RiskLevel),
		RequiresCheckpoint: tool.RequiresCheckpoint,
	}
}

func fromProtoJSONSchema(schema *emcpv1.JSONSchema) *emcp.JSONSchema {
	if schema == nil {
		return nil
	}

	result := &emcp.JSONSchema{
		Type:     schema.Type,
		Required: schema.Required,
	}

	if schema.Properties != nil {
		result.Properties = make(map[string]*emcp.SchemaProperty)
		for name, prop := range schema.Properties {
			result.Properties[name] = &emcp.SchemaProperty{
				Type:        prop.Type,
				Description: prop.Description,
				Enum:        prop.Enum,
				Format:      prop.Format,
			}
		}
	}

	return result
}

func fromProtoRiskLevel(level emcpv1.RiskLevel) emcp.RiskLevel {
	switch level {
	case emcpv1.RiskLevel_RISK_LEVEL_LOW:
		return emcp.RiskLow
	case emcpv1.RiskLevel_RISK_LEVEL_MEDIUM:
		return emcp.RiskMedium
	case emcpv1.RiskLevel_RISK_LEVEL_HIGH:
		return emcp.RiskHigh
	case emcpv1.RiskLevel_RISK_LEVEL_CRITICAL:
		return emcp.RiskCritical
	default:
		return emcp.RiskUnknown
	}
}

func fromProtoContent(content []*emcpv1.ContentItem) []emcp.ContentItem {
	result := make([]emcp.ContentItem, len(content))
	for i, item := range content {
		result[i] = emcp.ContentItem{
			Type:     item.Type,
			Text:     item.Text,
			Data:     item.Data,
			MimeType: item.MimeType,
			URI:      item.Uri,
		}
	}
	return result
}

// IsGRPCError checks if error is a gRPC error.
func IsGRPCError(err error) bool {
	_, ok := status.FromError(err)
	return ok
}

// GetGRPCCode extracts gRPC status code from error.
func GetGRPCCode(err error) codes.Code {
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}
