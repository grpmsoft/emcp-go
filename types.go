// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import "encoding/json"

// ToolInfo is a clean Go representation of an MCP tool definition.
// It deliberately avoids exposing any official SDK types in the public API.
type ToolInfo struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
	// OutputSchema is the schema of the tool's structured result; nil when the
	// tool declares none.
	OutputSchema map[string]any
	// Annotations are the tool's behaviour hints; nil when it declares none.
	Annotations *ToolAnnotations
}

// ToolResult is a clean Go representation of an MCP tool call result.
type ToolResult struct {
	// Content holds the content items returned by the tool.
	Content []ContentItem
	// StructuredContent holds the structured result of the tool call (per
	// SEP-2106) as raw JSON: any value conforming to the tool's OutputSchema,
	// an object, an array or a primitive. A client decodes it into its own
	// type with json.Unmarshal; a server setting it must give valid JSON.
	// Typed tools (AddTool) set it from their Out value. Nil when absent.
	StructuredContent json.RawMessage
	// IsError is true when the tool reported an application-level error.
	IsError bool
}

// ContentItem represents a single content item in a tool result.
// It supports all MCP content types: text, image, audio, resource_link,
// and resource (embedded resource).
type ContentItem struct {
	// Type is "text", "image", "audio", "resource_link", or "resource".
	Type string
	// Text holds the text content. Non-empty only when Type is "text".
	Text string
	// MIMEType is the MIME type for image/audio/resource content.
	MIMEType string
	// Data holds raw bytes for image/audio content.
	Data []byte

	// URI is the resource URI (resource_link and resource types).
	URI string
	// Name is the resource name (resource_link type).
	Name string
	// Title is the resource title (resource_link type).
	Title string
	// Description is the resource description (resource_link type).
	Description string

	// Resource holds the embedded resource contents (resource type).
	Resource *ResourceContents
}

// ResourceContents holds the contents of an embedded resource.
type ResourceContents struct {
	URI      string
	MIMEType string
	Text     string
	Blob     []byte
}
