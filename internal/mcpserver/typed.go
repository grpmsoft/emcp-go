// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TypedTool describes a tool registered with AddTypedTool.
type TypedTool struct {
	Name        string
	Title       string
	Description string
	// InputSchema and OutputSchema replace the schemas inferred from the
	// handler's In and Out types when non-nil.
	InputSchema  *jsonschema.Schema
	OutputSchema *jsonschema.Schema
	Annotations  *mcp.ToolAnnotations
}

// TypedHandler is the internal form of a typed tool handler.
type TypedHandler[In, Out any] func(ctx context.Context, in In) (*ToolResult, Out, error)

// AddTypedTool registers a tool whose arguments decode into In and whose
// structured result is Out. The SDK's generic AddTool does the work: it
// infers the schemas, validates the arguments (applying defaults) before the
// handler runs, reports validation and handler errors as tool errors, and
// sends Out as structured content with a JSON text fallback. Like the SDK, it
// panics on a tool that cannot be registered, such as an In type that is not
// a struct or map; that is a programming error found at startup.
func AddTypedTool[In, Out any](s *Server, t TypedTool, h TypedHandler[In, Out]) {
	tool := &mcp.Tool{
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
		Annotations: t.Annotations,
	}
	// The SDK fields are interfaces: a nil *jsonschema.Schema stored in them
	// would read as a provided schema, so only set the ones given.
	if t.InputSchema != nil {
		tool.InputSchema = t.InputSchema
	}
	if t.OutputSchema != nil {
		tool.OutputSchema = t.OutputSchema
	}

	mcp.AddTool(s.mcpServer, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, in)
		if err != nil || res == nil {
			return nil, out, err
		}
		result := toSDKResult(res)
		// Out is the structured result; the SDK sets it from out.
		result.StructuredContent = nil
		return result, out, nil
	})
}
