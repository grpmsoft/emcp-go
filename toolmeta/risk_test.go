package toolmeta

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRiskLevel(t *testing.T) {
	tests := []struct {
		name  string
		level RiskLevel
	}{
		{"low", RiskLow},
		{"medium", RiskMedium},
		{"high", RiskHigh},
		{"critical", RiskCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &mcp.Tool{Name: "test"}
			SetRiskLevel(tool, tt.level)

			got := GetRiskLevel(tool)
			if got != tt.level {
				t.Errorf("GetRiskLevel() = %q, want %q", got, tt.level)
			}
		})
	}
}

func TestRiskLevel_DefaultIsLow(t *testing.T) {
	tool := &mcp.Tool{Name: "test"}
	if got := GetRiskLevel(tool); got != RiskLow {
		t.Errorf("default risk = %q, want %q", got, RiskLow)
	}
}

func TestRiskLevel_NilMeta(t *testing.T) {
	tool := &mcp.Tool{Name: "test"}
	tool.Meta = nil
	if got := GetRiskLevel(tool); got != RiskLow {
		t.Errorf("nil meta risk = %q, want %q", got, RiskLow)
	}
}

func TestRequiresCheckpoint(t *testing.T) {
	tool := &mcp.Tool{Name: "test"}

	if RequiresCheckpoint(tool) {
		t.Error("default should be false")
	}

	SetRequiresCheckpoint(tool, true)
	if !RequiresCheckpoint(tool) {
		t.Error("should be true after set")
	}

	SetRequiresCheckpoint(tool, false)
	if RequiresCheckpoint(tool) {
		t.Error("should be false after unset")
	}
}

func TestMetaNamespacing(t *testing.T) {
	tool := &mcp.Tool{Name: "test"}
	SetRiskLevel(tool, RiskHigh)
	SetRequiresCheckpoint(tool, true)

	// Verify keys use emcp: prefix.
	if _, ok := tool.Meta["emcp:riskLevel"]; !ok {
		t.Error("missing emcp:riskLevel key")
	}
	if _, ok := tool.Meta["emcp:requiresCheckpoint"]; !ok {
		t.Error("missing emcp:requiresCheckpoint key")
	}

	// Verify no collision with non-prefixed keys.
	tool.Meta["riskLevel"] = "other"
	if GetRiskLevel(tool) != RiskHigh {
		t.Error("non-prefixed key should not affect GetRiskLevel")
	}
}

func TestSetMultipleAnnotations(t *testing.T) {
	tool := &mcp.Tool{Name: "delete_all"}
	SetRiskLevel(tool, RiskCritical)
	SetRequiresCheckpoint(tool, true)

	if GetRiskLevel(tool) != RiskCritical {
		t.Error("risk should be critical")
	}
	if !RequiresCheckpoint(tool) {
		t.Error("checkpoint should be required")
	}
}
