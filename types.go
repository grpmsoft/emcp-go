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
	// IsError is true when the tool reported an application-level error.
	IsError bool
}

// ContentItem represents a single content item in a tool result.
type ContentItem struct {
	// Type is "text", "image", or "audio".
	Type string
	// Text holds the text content. Non-empty only when Type is "text".
	Text string
	// MIMEType is the MIME type for image/audio content.
	MIMEType string
	// Data holds raw bytes for image/audio content.
	Data []byte
}
