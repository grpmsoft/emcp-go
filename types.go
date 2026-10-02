// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

// ToolInfo is a clean Go representation of an MCP tool definition.
// It deliberately avoids exposing any official SDK types in the public API.
type ToolInfo struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
}

// ToolResult is a clean Go representation of an MCP tool call result.
type ToolResult struct {
	// Content holds the content items returned by the tool.
	Content []ContentItem
	// StructuredContent holds the structured result of the tool call (per
	// SEP-2106). It may be any valid JSON value conforming to the tool's
	// OutputSchema.
	StructuredContent map[string]any
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
