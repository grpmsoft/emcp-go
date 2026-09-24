package middleware

import (
	"context"
	"fmt"

	"github.com/goco-ai/emcp-go/toolmeta"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MethodToolsCall is the JSON-RPC method name for tool invocations.
const MethodToolsCall = "tools/call"

// RiskPolicy decides whether a tool call should proceed based on its risk level.
// Returning a non-nil error blocks the call; the error is forwarded to the caller.
type RiskPolicy interface {
	Check(ctx context.Context, toolName string, riskLevel toolmeta.RiskLevel) error
}

// RiskGate returns a middleware that enforces a [RiskPolicy] on tool calls.
//
// The toolRisks map associates tool names with their declared risk levels.
// Tools not present in the map are treated as [toolmeta.RiskLow].
//
// Only "tools/call" requests are inspected; all other methods pass through
// unchanged.
func RiskGate(policy RiskPolicy, toolRisks map[string]toolmeta.RiskLevel) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != MethodToolsCall {
				return next(ctx, method, req)
			}

			toolName := extractToolName(req)
			if toolName == "" {
				return next(ctx, method, req)
			}

			level, ok := toolRisks[toolName]
			if !ok {
				level = toolmeta.RiskLow
			}

			if err := policy.Check(ctx, toolName, level); err != nil {
				return nil, err
			}

			return next(ctx, method, req)
		}
	}
}

// riskOrder maps risk levels to a comparable integer for threshold comparison.
var riskOrder = map[toolmeta.RiskLevel]int{
	toolmeta.RiskLow:      0,
	toolmeta.RiskMedium:   1,
	toolmeta.RiskHigh:     2,
	toolmeta.RiskCritical: 3,
}

// MaxRiskPolicy blocks tool calls whose risk level exceeds a configured
// maximum. Calls at or below the maximum are allowed.
type MaxRiskPolicy struct {
	MaxLevel toolmeta.RiskLevel
}

// Check returns a JSON-RPC error if the tool's risk level exceeds the policy maximum.
func (p *MaxRiskPolicy) Check(_ context.Context, toolName string, level toolmeta.RiskLevel) error {
	maxOrd := riskOrder[p.MaxLevel]
	levelOrd := riskOrder[level]
	if levelOrd > maxOrd {
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidRequest,
			Message: fmt.Sprintf("tool %q blocked: risk level %q exceeds maximum %q", toolName, level, p.MaxLevel),
		}
	}
	return nil
}

// extractToolName pulls the tool name from a tools/call request.
// It uses the SDK's typed request when possible, falling back gracefully.
func extractToolName(req mcp.Request) string {
	if ctr, ok := req.(*mcp.CallToolRequest); ok {
		return ctr.Params.Name
	}
	return ""
}
