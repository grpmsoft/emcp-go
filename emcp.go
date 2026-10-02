// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Package emcp provides a unified MCP client that wraps the official
// MCP Go SDK and the emcp-go gRPC transport. Consumers import emcp-go
// only -- the official SDK types are never exposed in the public API.
//
// Usage:
//
//	client, err := emcp.NewClient(emcp.Config{
//	    Endpoint:  "http://localhost:8094/mcp",
//	    Transport: emcp.TransportHTTP,
//	})
//	defer client.Close()
//
//	tools, err := client.ListTools(ctx)
//	result, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hi"})
package emcp

import (
	"context"
	"fmt"

	"github.com/grpmsoft/emcp-go/internal/mcpclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client is a unified MCP client that supports HTTP, gRPC, and stdio
// transports. It delegates all work to the internal mcpclient implementation.
type Client struct {
	impl *mcpclient.Client
}

// NewClient creates a new MCP client with the given configuration. It
// resolves PID file discovery, validates the config, and eagerly connects
// to the server.
func NewClient(cfg Config) (*Client, error) {
	// Resolve PID file if provided.
	if cfg.PIDFile != "" && cfg.Endpoint == "" {
		info, err := mcpclient.ReadPIDFile(cfg.PIDFile)
		if err != nil {
			return nil, fmt.Errorf("reading pid file: %w", err)
		}
		if info.Port == 0 {
			return nil, fmt.Errorf("pid file %s has no port", cfg.PIDFile)
		}
		if cfg.Transport == "" {
			cfg.Transport = TransportHTTP
		}
		switch cfg.Transport {
		case TransportHTTP, TransportAuto:
			cfg.Endpoint = fmt.Sprintf("http://localhost:%d/mcp", info.Port)
		case TransportGRPC:
			cfg.Endpoint = fmt.Sprintf("localhost:%d", info.Port)
		}
		if info.Token != "" && cfg.BearerToken == "" {
			cfg.BearerToken = info.Token
		}
	}

	impl, err := mcpclient.New(mcpclient.Config{
		Endpoint:    cfg.Endpoint,
		PIDFile:     cfg.PIDFile,
		Transport:   mcpclient.Transport(cfg.Transport),
		Command:     cfg.Command,
		Args:        cfg.Args,
		BearerToken: cfg.BearerToken,
		Timeout:     cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}

	return &Client{impl: impl}, nil
}

// ListTools returns all tools available on the connected MCP server.
func (c *Client) ListTools(ctx context.Context) ([]*ToolInfo, error) {
	result, err := c.impl.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	tools := make([]*ToolInfo, 0, len(result.Tools))
	for _, t := range result.Tools {
		info := &ToolInfo{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
		}
		if m, ok := t.InputSchema.(map[string]any); ok {
			info.InputSchema = m
		}
		tools = append(tools, info)
	}
	return tools, nil
}

// CallTool invokes a tool by name with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	result, err := c.impl.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return convertCallToolResult(result), nil
}

// Ping sends a ping to the server to verify the connection is alive.
func (c *Client) Ping(ctx context.Context) error {
	return c.impl.Ping(ctx)
}

// Close shuts down the client and releases all resources.
func (c *Client) Close() error {
	return c.impl.Close()
}

// clearSession clears the internal session, forcing a reconnect on the next
// call. Exported only within the package for test support.
func (c *Client) clearSession() {
	c.impl.ClearSession()
}

// convertCallToolResult converts an SDK CallToolResult to our clean ToolResult.
func convertCallToolResult(r *mcp.CallToolResult) *ToolResult {
	result := &ToolResult{
		IsError: r.IsError,
	}
	for _, c := range r.Content {
		result.Content = append(result.Content, convertContent(c))
	}
	return result
}

// convertContent converts an SDK Content to our clean ContentItem.
func convertContent(c mcp.Content) ContentItem {
	switch v := c.(type) {
	case *mcp.TextContent:
		return ContentItem{
			Type: "text",
			Text: v.Text,
		}
	case *mcp.ImageContent:
		return ContentItem{
			Type:     "image",
			Data:     []byte(v.Data),
			MIMEType: v.MIMEType,
		}
	case *mcp.AudioContent:
		return ContentItem{
			Type:     "audio",
			Data:     []byte(v.Data),
			MIMEType: v.MIMEType,
		}
	default:
		return ContentItem{Type: "text"}
	}
}
