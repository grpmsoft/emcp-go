// Package emcp provides Enhanced Model Context Protocol implementation.
// 100% compatible with Anthropic MCP 1.0+ with enterprise extensions.
package emcp

import (
	"context"
	"encoding/json"
	"time"
)

// Version constants following semantic versioning.
const (
	Version         = "0.1.0"
	ProtocolVersion = "1.0.0" // MCP protocol version
	EMCPVersion     = "0.1.0" // eMCP extensions version
)

// Implementation describes server implementation details.
// MCP 1.0 compatible.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo contains server metadata.
// MCP 1.0 compatible with eMCP extensions.
type ServerInfo struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	ProtocolVersion string        `json:"protocolVersion"`
	Capabilities   Capabilities   `json:"capabilities"`
	Implementation Implementation `json:"implementation,omitempty"`
	
	// eMCP extensions (optional, backward compatible)
	EMCPVersion string                 `json:"emcpVersion,omitempty"`
	Extensions  map[string]interface{} `json:"extensions,omitempty"`
}

// Capabilities defines server capabilities.
// MCP 1.0 standard.
type Capabilities struct {
	Tools         *ToolsCapability      `json:"tools,omitempty"`
	Resources     *ResourcesCapability  `json:"resources,omitempty"`
	Prompts       *PromptsCapability    `json:"prompts,omitempty"`
	Logging       *LoggingCapability    `json:"logging,omitempty"`
	
	// eMCP extensions
	Experimental  map[string]interface{} `json:"experimental,omitempty"`
}

// Capability structs for MCP 1.0
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

// Tool definition with type-safe handler.
// Generic version for compile-time type checking.
type Tool[TInput, TOutput any] struct {
	Name        string                                       `json:"name"`
	Description string                                       `json:"description,omitempty"`
	InputSchema *JSONSchema                                  `json:"inputSchema"`
	Handler     func(context.Context, TInput) (TOutput, error) `json:"-"`
	
	// eMCP extensions (backward compatible - ignored by MCP 1.0 clients)
	RiskLevel          RiskLevel               `json:"riskLevel,omitempty"`
	RequiresCheckpoint bool                    `json:"requiresCheckpoint,omitempty"`
	Metadata           map[string]interface{}  `json:"metadata,omitempty"`
}

// ToolDefinition is type-erased tool for runtime.
// Used for MCP 1.0 wire format.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema *JSONSchema            `json:"inputSchema"`
	
	// eMCP extensions
	RiskLevel          RiskLevel              `json:"riskLevel,omitempty"`
	RequiresCheckpoint bool                   `json:"requiresCheckpoint,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// JSONSchema represents JSON Schema for tool inputs.
// MCP 1.0 standard.
type JSONSchema struct {
	Type                 string                    `json:"type"`
	Properties           map[string]*SchemaProperty `json:"properties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	AdditionalProperties interface{}               `json:"additionalProperties,omitempty"`
}

// SchemaProperty defines a property in JSON Schema.
type SchemaProperty struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Format      string      `json:"format,omitempty"`
}

// Resource definition - MCP 1.0 standard.
type Resource struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mimeType,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Prompt template - MCP 1.0 standard.
type Prompt struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Arguments   []PromptArgument  `json:"arguments,omitempty"`
}

// PromptArgument defines template argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// RiskLevel for eMCP risk assessment.
// Extension to MCP 1.0 (backward compatible).
type RiskLevel string

const (
	RiskUnknown  RiskLevel = "unknown"
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// String implements fmt.Stringer.
func (r RiskLevel) String() string {
	return string(r)
}

// MarshalJSON implements json.Marshaler.
func (r RiskLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *RiskLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*r = RiskLevel(s)
	return nil
}

// CallToolResult represents tool execution result.
// MCP 1.0 compatible with eMCP extensions.
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
	
	// eMCP extensions
	ExecutionTime time.Duration          `json:"executionTime,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ContentItem represents content in various formats.
// MCP 1.0 standard.
type ContentItem struct {
	Type     string      `json:"type"` // "text", "image", "resource"
	Text     string      `json:"text,omitempty"`
	Data     string      `json:"data,omitempty"`
	MimeType string      `json:"mimeType,omitempty"`
	URI      string      `json:"uri,omitempty"`
}

// Request represents JSON-RPC 2.0 request.
// MCP 1.0 protocol.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents JSON-RPC 2.0 response.
// MCP 1.0 protocol.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents JSON-RPC 2.0 error.
// MCP 1.0 standard error codes + eMCP extensions.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes (MCP 1.0)
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// eMCP extension error codes (-32000 to -32099 reserved)
const (
	ErrCodeRiskAssessmentFailed = -32000
	ErrCodeCheckpointRequired   = -32001
	ErrCodeRateLimitExceeded    = -32002
)

// Error implements error interface.
func (e *Error) Error() string {
	return e.Message
}

// NewError creates standard error.
func NewError(code int, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// NewErrorWithData creates error with additional data.
func NewErrorWithData(code int, message string, data interface{}) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// Type aliases for backward compatibility and clarity
type (
	JSONRPCRequest  = Request
	JSONRPCResponse = Response
	JSONRPCError    = Error
)

// ResourceDefinition represents an MCP resource
type ResourceDefinition struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mimeType,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PromptDefinition represents an MCP prompt
type PromptDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Arguments   []PromptArgument       `json:"arguments,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}


// Deprecated: Use RiskLow instead
const RiskLevelLow = RiskLow

// Deprecated: Use RiskMedium instead
const RiskLevelMedium = RiskMedium

// Deprecated: Use RiskHigh instead
const RiskLevelHigh = RiskHigh

// Deprecated: Use RiskCritical instead
const RiskLevelCritical = RiskCritical
