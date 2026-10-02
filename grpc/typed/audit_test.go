// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Audit regression tests (2026-10-02), see AUDIT-20261002.md (M6).
// If the decision is to drop grpc/typed, delete this file with the package.
package typed

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// M6: MCP defaults destructiveHint and openWorldHint to TRUE when absent.
// A nil pointer must not be advertised as false.
func TestAudit_M6_AnnotationDefaults(t *testing.T) {
	pt, err := mcpToolToProto(&mcp.Tool{
		Name:        "rm",
		InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{}, // nothing set
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pt.Annotations.DestructiveHint {
		t.Error("nil DestructiveHint advertised as false; spec default is true")
	}
	if !pt.Annotations.OpenWorldHint {
		t.Error("nil OpenWorldHint advertised as false; spec default is true")
	}
}

// M6: ListTools must be deterministic (sorted by name).
func TestAudit_M6_ListToolsDeterministic(t *testing.T) {
	h := NewTypedGRPCHandler()
	names := []string{"h", "c", "a", "f", "b", "e", "g", "d"}
	for _, n := range names {
		h.AddTool(&mcp.Tool{Name: n, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, json.RawMessage) (*mcp.CallToolResult, error) { return &mcp.CallToolResult{}, nil })
	}
	sort.Strings(names)
	for i := 0; i < 20; i++ {
		resp, err := h.ListTools(context.Background(), &mcppb.ListToolsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for j, tl := range resp.Tools {
			if tl.Name != names[j] {
				t.Fatalf("call %d: tools[%d]=%q, want %q (unsorted map iteration)", i, j, tl.Name, names[j])
			}
		}
	}
}

// M6: if the package stays, it must speak the canonical schema
// (GoogleCloudPlatform/mcp-grpc-transport-proto): service
// model_context_protocol.Mcp, not mcppb.Mcp.
func TestAudit_M6_CanonicalServiceName(t *testing.T) {
	if got := mcppb.Mcp_ServiceDesc.ServiceName; got != "model_context_protocol.Mcp" {
		t.Errorf("service name %q is not the canonical model_context_protocol.Mcp; no canonical client can reach this server", got)
	}
}
