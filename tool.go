// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/grpmsoft/emcp-go/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool describes a typed tool registered with AddTool.
//
// The schemas use *jsonschema.Schema from github.com/google/jsonschema-go, the
// JSON Schema package the official MCP SDK uses too. It is the one type from
// outside emcp-go in this API: a schema type of our own would only mirror it.
type Tool struct {
	// Name identifies the tool; required.
	Name string
	// Title is a human-readable name for display.
	Title string
	// Description tells the model what the tool does and returns. Details of
	// each argument belong in the input schema's property descriptions.
	Description string
	// InputSchema replaces the schema inferred from the handler's In type.
	// Start from InferSchema to add what struct tags cannot express: enums,
	// ranges, defaults. Nil means the inferred schema.
	InputSchema *jsonschema.Schema
	// OutputSchema replaces the schema inferred from the handler's Out type.
	// Nil means the inferred schema; none when Out is any.
	OutputSchema *jsonschema.Schema
	// Annotations are hints about the tool's behaviour for clients.
	Annotations *ToolAnnotations
}

// ToolAnnotations are hints about a tool's behaviour (MCP tool annotations).
// Clients may use them for confirmation prompts; they are not guarantees.
type ToolAnnotations struct {
	// ReadOnlyHint: the tool does not modify its environment. Default false.
	ReadOnlyHint bool
	// DestructiveHint: the tool may make destructive updates. Meaningful only
	// when ReadOnlyHint is false. Nil means the spec default, true.
	DestructiveHint *bool
	// IdempotentHint: repeated calls with the same arguments have no further
	// effect. Meaningful only when ReadOnlyHint is false. Default false.
	IdempotentHint bool
	// OpenWorldHint: the tool interacts with an open world of external
	// entities, such as the web. Nil means the spec default, true.
	OpenWorldHint *bool
}

// TypedToolHandler handles a call of a tool registered with AddTool.
//
// in holds the call's arguments, already validated against the input schema
// with its defaults applied. The returned Out is the structured result: it is
// sent as structured content and, when the result has no content of its own,
// as JSON text. Return a non-nil *ToolResult only for content beyond that,
// such as an image; its StructuredContent is ignored.
//
// A returned error is reported to the client as a tool error (IsError with
// the error's text), as are arguments that fail validation, so the model sees
// why the call failed. A cancelled ctx surfaces the same way, as its error.
type TypedToolHandler[In, Out any] func(ctx context.Context, in In) (*ToolResult, Out, error)

// AddTool registers a typed tool on s. In must be a struct or a map: its JSON
// Schema, inferred from the Go type with property descriptions from the
// `jsonschema` struct tag, becomes the tool's input schema unless t sets one.
// Unknown properties are rejected. Fields without omitempty or omitzero in
// their json tag are required.
//
// Typed tools are served by HTTPHandler and GRPCHandler like tools added with
// Server.AddTool, behind the same authentication. The experimental grpc/typed
// transport keeps its own registry and does not see them.
//
// AddTool panics when the tool cannot be registered, for example when In is
// not a struct or map or a given schema is invalid: a programming error that
// shows at startup.
func AddTool[In, Out any](s *Server, t Tool, h TypedToolHandler[In, Out]) {
	mcpserver.AddTypedTool(s.impl, mcpserver.TypedTool{
		Name:         t.Name,
		Title:        t.Title,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: t.OutputSchema,
		Annotations:  t.Annotations.toSDK(),
	}, func(ctx context.Context, in In) (*mcpserver.ToolResult, Out, error) {
		res, out, err := h(ctx, in)
		if err != nil || res == nil {
			return nil, out, err
		}
		return toInternalResult(res), out, nil
	})
}

// InferSchema returns the JSON Schema that AddTool infers for T, as a starting
// point for Tool.InputSchema or Tool.OutputSchema.
func InferSchema[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](nil)
}

func (a *ToolAnnotations) toSDK() *mcp.ToolAnnotations {
	if a == nil {
		return nil
	}
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
}

func toolAnnotationsFromSDK(a *mcp.ToolAnnotations) *ToolAnnotations {
	if a == nil {
		return nil
	}
	return &ToolAnnotations{
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
}
