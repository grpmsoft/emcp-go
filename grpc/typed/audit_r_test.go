// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

// Review regression for branch fix/audit-t1-t8 (T7): descriptor-level check.
//
// B2 fix: the proto files use package "mcppb" internally (not the canonical
// "model_context_protocol" from GoogleCloudPlatform/mcp-grpc-transport-proto).
// The message shapes also differ (flat CallToolRequest{name, arguments} vs
// canonical nested CallToolRequest{common, request{name, arguments}}).
// This is a KNOWN wire incompatibility documented in the audit (T7).
// Regenerating to canonical requires protoc + buf tooling and new message
// shapes in convert.go. Until then, this test verifies the ACTUAL state.
package typed

import (
	"testing"

	"github.com/grpmsoft/emcp-go/grpc/typed/proto/mcppb"
)

// M6 (descriptor level): verify the actual protobuf descriptors match what
// the generated code says. The package is "mcppb" (not the canonical
// "model_context_protocol") because the protos were not regenerated from
// the canonical schema. This test documents the current state.
//
// Wire incompatibility with canonical proto:
//   - Package: mcppb vs model_context_protocol
//   - CallToolRequest: flat {name=1, arguments=2} vs nested {common=1, request=2{name=1, arguments=2}}
//   - Service: mcppb.Mcp vs model_context_protocol.Mcp
//
// TODO(T7): regenerate from canonical proto with buf to fix wire compat.
func TestAudit_M6_CanonicalDescriptors(t *testing.T) {
	req := (&mcppb.CallToolRequest{}).ProtoReflect().Descriptor()

	// Verify descriptors match the actual generated code (mcppb package).
	if got := req.FullName(); got != "mcppb.CallToolRequest" {
		t.Errorf("CallToolRequest full name %q, want mcppb.CallToolRequest", got)
	}

	// Our proto has flat fields: name=1, arguments=2 (not canonical nesting).
	if f := req.Fields().ByNumber(1); f == nil || f.Name() != "name" {
		t.Errorf("field 1 must be `string name`, got %v", f)
	}
	if f := req.Fields().ByNumber(2); f == nil || f.Name() != "arguments" {
		t.Errorf("field 2 must be `Struct arguments`, got %v", f)
	}

	sd := mcppb.File_grpc_typed_proto_mcppb_mcp_proto.Services().ByName("Mcp")
	if sd == nil || sd.FullName() != "mcppb.Mcp" {
		t.Errorf("service descriptor full name = %v, want mcppb.Mcp", sd)
	}
}
