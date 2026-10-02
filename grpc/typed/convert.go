// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package typed

import (
	"encoding/json"
	"fmt"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/structpb"
)

// ToolInfo is a clean Go representation of a tool definition returned by
// the typed client. It deliberately avoids exposing proto types in the
// public API.
type ToolInfo struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
}

// ToolResult is a clean Go representation of a tool call result returned
// by the typed client.
type ToolResult struct {
	// Content holds the textual or other content items returned by the tool.
	Content []ContentItem
	// IsError is true when the tool reported an application-level error.
	IsError bool
}

// ContentItem represents a single content item in a tool result.
// Currently only text is supported; image and audio fields are reserved
// for future use.
type ContentItem struct {
	Type        string // "text", "image", "audio", "embedded_resource", "resource_link"
	Text        string
	MIMEType    string
	Data        []byte
	URI         string
	Name        string
	Title       string
	Description string
}

// --- MCP Tool -> Proto Tool ---

// mcpToolToProto converts an mcp.Tool to a proto Tool message.
func mcpToolToProto(t *mcp.Tool) (*mcppb.Tool, error) {
	pt := &mcppb.Tool{
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
	}

	// Convert InputSchema (any) -> proto Struct.
	if t.InputSchema != nil {
		s, err := anyToProtoStruct(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("converting input schema for tool %q: %w", t.Name, err)
		}
		pt.InputSchema = s
	}

	// Convert OutputSchema (any) -> proto Struct.
	if t.OutputSchema != nil {
		s, err := anyToProtoStruct(t.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("converting output schema for tool %q: %w", t.Name, err)
		}
		pt.OutputSchema = s
	}

	// Convert annotations. Per the MCP spec, DestructiveHint and OpenWorldHint
	// default to TRUE when absent (nil), while ReadOnlyHint and IdempotentHint
	// default to FALSE.
	if t.Annotations != nil {
		pt.Annotations = &mcppb.ToolAnnotations{
			Title:          t.Annotations.Title,
			ReadOnlyHint:   t.Annotations.ReadOnlyHint,
			IdempotentHint: t.Annotations.IdempotentHint,
		}
		if t.Annotations.DestructiveHint != nil {
			pt.Annotations.DestructiveHint = *t.Annotations.DestructiveHint
		} else {
			pt.Annotations.DestructiveHint = true // spec default
		}
		if t.Annotations.OpenWorldHint != nil {
			pt.Annotations.OpenWorldHint = *t.Annotations.OpenWorldHint
		} else {
			pt.Annotations.OpenWorldHint = true // spec default
		}
	}

	return pt, nil
}

// --- Proto Tool -> ToolInfo ---

// protoToolToInfo converts a proto Tool message to our clean ToolInfo type.
func protoToolToInfo(pt *mcppb.Tool) *ToolInfo {
	info := &ToolInfo{
		Name:        pt.GetName(),
		Title:       pt.GetTitle(),
		Description: pt.GetDescription(),
	}
	if pt.GetInputSchema() != nil {
		info.InputSchema = pt.GetInputSchema().AsMap()
	}
	return info
}

// --- Proto CallToolRequest -> tool name + JSON args ---

// protoCallToolArgs extracts the tool name and JSON-encoded arguments from
// a proto CallToolRequest. The canonical proto nests name and arguments
// inside a Request sub-message (field 2).
func protoCallToolArgs(req *mcppb.CallToolRequest) (name string, argsJSON json.RawMessage, err error) {
	inner := req.GetRequest()
	if inner == nil {
		return "", nil, fmt.Errorf("CallToolRequest.request is required")
	}
	name = inner.GetName()
	if name == "" {
		return "", nil, fmt.Errorf("tool name is required")
	}

	if inner.GetArguments() != nil {
		argsJSON, err = json.Marshal(inner.GetArguments().AsMap())
		if err != nil {
			return "", nil, fmt.Errorf("marshaling arguments: %w", err)
		}
	} else {
		argsJSON = json.RawMessage("{}")
	}
	return name, argsJSON, nil
}

// --- CallToolResult -> Proto CallToolResponse ---

// callToolResultToProto converts an mcp.CallToolResult to a proto
// CallToolResponse.
func callToolResultToProto(result *mcp.CallToolResult) *mcppb.CallToolResponse {
	resp := &mcppb.CallToolResponse{
		IsError: result.IsError,
	}
	for _, c := range result.Content {
		resp.Content = append(resp.Content, contentToProto(c))
	}
	return resp
}

// --- Proto CallToolResponse -> ToolResult ---

// protoCallToolResponseToResult converts a proto CallToolResponse to our
// clean ToolResult type.
func protoCallToolResponseToResult(resp *mcppb.CallToolResponse) *ToolResult {
	result := &ToolResult{
		IsError: resp.GetIsError(),
	}
	for _, pc := range resp.GetContent() {
		result.Content = append(result.Content, protoContentToItem(pc))
	}
	return result
}

// --- Content converters ---

// contentToProto converts an mcp.Content interface to a proto Content message.
func contentToProto(c mcp.Content) *mcppb.CallToolResponse_Content {
	pc := &mcppb.CallToolResponse_Content{}
	switch v := c.(type) {
	case *mcp.TextContent:
		pc.Text = &mcppb.TextContent{Text: v.Text}
	case *mcp.ImageContent:
		pc.Image = &mcppb.ImageContent{
			Data:     v.Data,
			MimeType: v.MIMEType,
		}
	case *mcp.AudioContent:
		pc.Audio = &mcppb.AudioContent{
			Data:     v.Data,
			MimeType: v.MIMEType,
		}
	case *mcp.EmbeddedResource:
		er := &mcppb.EmbeddedResource{}
		if v.Resource != nil {
			er.Contents = &mcppb.ResourceContents{
				Uri:      v.Resource.URI,
				MimeType: v.Resource.MIMEType,
				Text:     v.Resource.Text,
				Blob:     v.Resource.Blob,
			}
		}
		pc.EmbeddedResource = er
	case *mcp.ResourceLink:
		pc.ResourceLink = &mcppb.Resource{
			Uri:         v.URI,
			Name:        v.Name,
			Title:       v.Title,
			Description: v.Description,
			MimeType:    v.MIMEType,
		}
	}
	return pc
}

// protoContentToItem converts a proto Content message to our clean ContentItem.
func protoContentToItem(pc *mcppb.CallToolResponse_Content) ContentItem {
	switch {
	case pc.GetText() != nil:
		return ContentItem{
			Type: "text",
			Text: pc.GetText().GetText(),
		}
	case pc.GetImage() != nil:
		return ContentItem{
			Type:     "image",
			Data:     pc.GetImage().GetData(),
			MIMEType: pc.GetImage().GetMimeType(),
		}
	case pc.GetAudio() != nil:
		return ContentItem{
			Type:     "audio",
			Data:     pc.GetAudio().GetData(),
			MIMEType: pc.GetAudio().GetMimeType(),
		}
	case pc.GetEmbeddedResource() != nil:
		item := ContentItem{Type: "embedded_resource"}
		if c := pc.GetEmbeddedResource().GetContents(); c != nil {
			item.URI = c.GetUri()
			item.MIMEType = c.GetMimeType()
			item.Text = c.GetText()
			item.Data = c.GetBlob()
		}
		return item
	case pc.GetResourceLink() != nil:
		r := pc.GetResourceLink()
		return ContentItem{
			Type:        "resource_link",
			URI:         r.GetUri(),
			Name:        r.GetName(),
			Title:       r.GetTitle(),
			Description: r.GetDescription(),
			MIMEType:    r.GetMimeType(),
		}
	default:
		return ContentItem{Type: "text"}
	}
}

// --- map[string]any <-> proto Struct ---

// mapToProtoStruct converts a map[string]any to a proto Struct.
func mapToProtoStruct(m map[string]any) (*structpb.Struct, error) {
	return structpb.NewStruct(m)
}

// anyToProtoStruct converts an arbitrary Go value to a proto Struct by
// round-tripping through JSON. This handles jsonschema.Schema, map[string]any,
// json.RawMessage, and any other JSON-serializable type.
func anyToProtoStruct(v any) (*structpb.Struct, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling to JSON: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON to map: %w", err)
	}
	return structpb.NewStruct(m)
}
