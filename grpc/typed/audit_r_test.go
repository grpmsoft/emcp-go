// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Descriptor-level verification for D2-A canonical proto.
// Proto is now model_context_protocol (Google canonical), 8 RPCs.
package typed

import (
	"testing"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
)

// Verify protobuf descriptors match canonical model_context_protocol schema.
func TestAudit_M6_CanonicalDescriptors(t *testing.T) {
	req := (&mcppb.CallToolRequest{}).ProtoReflect().Descriptor()

	if got := req.FullName(); got != "model_context_protocol.CallToolRequest" {
		t.Errorf("CallToolRequest full name %q, want model_context_protocol.CallToolRequest", got)
	}

	// Canonical proto has nested structure: common=1 (RequestFields), request=2
	if f := req.Fields().ByNumber(1); f == nil || f.Name() != "common" {
		t.Errorf("field 1 must be `RequestFields common`, got %v", f)
	}
	if f := req.Fields().ByNumber(2); f == nil || f.Name() != "request" {
		t.Errorf("field 2 must be `Request request`, got %v", f)
	}

	sd := mcppb.File_mcp_proto.Services().ByName("Mcp")
	if sd == nil || sd.FullName() != "model_context_protocol.Mcp" {
		t.Errorf("service descriptor full name = %v, want model_context_protocol.Mcp", sd)
	}
}
