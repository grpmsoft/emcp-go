// Package toolmeta provides enterprise metadata helpers for MCP tools.
// Annotations are stored in Tool.Meta (map[string]any) with "emcp:" namespace prefix,
// ensuring no collisions with standard MCP fields or other extensions.
package toolmeta

import "github.com/modelcontextprotocol/go-sdk/mcp"

// RiskLevel classifies the risk of executing a tool.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

const (
	keyRiskLevel          = "emcp:riskLevel"
	keyRequiresCheckpoint = "emcp:requiresCheckpoint"
)

// SetRiskLevel sets the enterprise risk level on a tool's Meta.
func SetRiskLevel(t *mcp.Tool, level RiskLevel) {
	ensureMeta(t)
	t.Meta[keyRiskLevel] = string(level)
}

// GetRiskLevel reads the enterprise risk level from a tool's Meta.
// Returns RiskLow if not set.
func GetRiskLevel(t *mcp.Tool) RiskLevel {
	if t.Meta == nil {
		return RiskLow
	}
	v, ok := t.Meta[keyRiskLevel].(string)
	if !ok {
		return RiskLow
	}
	return RiskLevel(v)
}

// SetRequiresCheckpoint marks a tool as requiring a checkpoint before execution.
func SetRequiresCheckpoint(t *mcp.Tool, required bool) {
	ensureMeta(t)
	t.Meta[keyRequiresCheckpoint] = required
}

// RequiresCheckpoint returns whether a tool requires a checkpoint before execution.
func RequiresCheckpoint(t *mcp.Tool) bool {
	if t.Meta == nil {
		return false
	}
	v, _ := t.Meta[keyRequiresCheckpoint].(bool)
	return v
}

func ensureMeta(t *mcp.Tool) {
	if t.Meta == nil {
		t.Meta = make(mcp.Meta)
	}
}
