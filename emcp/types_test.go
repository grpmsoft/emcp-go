package emcp

import (
	"encoding/json/v2"
	"testing"
)

func TestRiskLevel_String(t *testing.T) {
	tests := []struct {
		level RiskLevel
		want  string
	}{
		{RiskLevelLow, "low"},
		{RiskLevelMedium, "medium"},
		{RiskLevelHigh, "high"},
		{RiskLevelCritical, "critical"},
		{RiskLevel("invalid"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("RiskLevel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRiskLevel_MarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		level RiskLevel
		want  string
	}{
		{"low", RiskLevelLow, `"low"`},
		{"medium", RiskLevelMedium, `"medium"`},
		{"high", RiskLevelHigh, `"high"`},
		{"critical", RiskLevelCritical, `"critical"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.level)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestRiskLevel_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    RiskLevel
		wantErr bool
	}{
		{"low", `"low"`, RiskLevelLow, false},
		{"medium", `"medium"`, RiskLevelMedium, false},
		{"high", `"high"`, RiskLevelHigh, false},
		{"critical", `"critical"`, RiskLevelCritical, false},
		{"unknown", `"unknown"`, RiskLevelMedium, false}, // defaults to medium
		{"invalid", `"invalid"`, RiskLevelMedium, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RiskLevel
			err := json.Unmarshal([]byte(tt.json), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("json.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("json.Unmarshal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServerInfo_JSON(t *testing.T) {
	info := ServerInfo{
		Name:            "test-server",
		Version:         "1.0.0",
		ProtocolVersion: "1.0",
	}

	// Marshal
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal
	var decoded ServerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify
	if decoded.Name != info.Name {
		t.Errorf("Name = %v, want %v", decoded.Name, info.Name)
	}
	if decoded.Version != info.Version {
		t.Errorf("Version = %v, want %v", decoded.Version, info.Version)
	}
	if decoded.ProtocolVersion != info.ProtocolVersion {
		t.Errorf("ProtocolVersion = %v, want %v", decoded.ProtocolVersion, info.ProtocolVersion)
	}
}

func TestToolDefinition_JSON_WithEMCP(t *testing.T) {
	tool := ToolDefinition{
		Name:               "test_tool",
		Description:        "Test tool",
		InputSchema:        &JSONSchema{Type: "object"},
		RiskLevel:          RiskLevelHigh,
		RequiresCheckpoint: true,
		Metadata: map[string]interface{}{
			"version": "1.0",
		},
	}

	// Marshal
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify eMCP fields present
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded["riskLevel"] != "high" {
		t.Errorf("riskLevel = %v, want high", decoded["riskLevel"])
	}
	if decoded["requiresCheckpoint"] != true {
		t.Errorf("requiresCheckpoint = %v, want true", decoded["requiresCheckpoint"])
	}
}

func TestToolDefinition_JSON_WithoutEMCP(t *testing.T) {
	// Standard MCP tool without eMCP extensions
	tool := ToolDefinition{
		Name:        "standard_tool",
		Description: "Standard MCP tool",
		InputSchema: &JSONSchema{Type: "object"},
		// No eMCP fields
	}

	// Marshal
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify eMCP fields omitted (omitempty)
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, exists := decoded["riskLevel"]; exists {
		t.Error("riskLevel should be omitted when empty")
	}
	if _, exists := decoded["requiresCheckpoint"]; exists {
		t.Error("requiresCheckpoint should be omitted when false")
	}
	if _, exists := decoded["metadata"]; exists {
		t.Error("metadata should be omitted when empty")
	}
}

func TestJSONRPCRequest_JSON(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "test-123",
		Method:  "tools/list",
		Params:  []byte(`{}`),
	}

	// Marshal
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal
	var decoded JSONRPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify
	if decoded.JSONRPC != req.JSONRPC {
		t.Errorf("JSONRPC = %v, want %v", decoded.JSONRPC, req.JSONRPC)
	}
	if decoded.Method != req.Method {
		t.Errorf("Method = %v, want %v", decoded.Method, req.Method)
	}
}

func TestJSONRPCError_ErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{"parse error", ErrCodeParse, "Parse error"},
		{"invalid request", ErrCodeInvalidRequest, "Invalid request"},
		{"method not found", ErrCodeMethodNotFound, "Method not found"},
		{"invalid params", ErrCodeInvalidParams, "Invalid params"},
		{"internal error", ErrCodeInternal, "Internal error"},
		{"risk assessment", ErrCodeRiskAssessmentFailed, "Risk assessment failed"},
		{"checkpoint required", ErrCodeCheckpointRequired, "Checkpoint required"},
		{"rate limit", ErrCodeRateLimitExceeded, "Rate limit exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := JSONRPCError{
				Code:    tt.code,
				Message: tt.want,
			}

			// Verify code
			if err.Code != tt.code {
				t.Errorf("Code = %v, want %v", err.Code, tt.code)
			}
		})
	}
}