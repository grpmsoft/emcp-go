// Package server provides middleware implementations
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LoggingMiddleware logs tool calls with execution time
func LoggingMiddleware(logger func(format string, args ...any)) ToolMiddleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			start := time.Now()
			result, err := next(ctx, params)
			duration := time.Since(start)

			if err != nil {
				logger("tool call failed in %v: %v", duration, err)
			} else {
				logger("tool call completed in %v", duration)
			}

			return result, err
		}
	}
}

// RiskAssessmentMiddleware checks risk level before execution (eMCP extension)
type RiskPolicy struct {
	// MaxRiskLevel is the maximum allowed risk level (low, medium, high, critical)
	MaxRiskLevel string
	// OnRiskExceeded is called when risk level is exceeded
	OnRiskExceeded func(ctx context.Context, toolName string, riskLevel string) error
}

// RiskAssessmentMiddleware creates middleware for risk assessment
func RiskAssessmentMiddleware(policy RiskPolicy) ToolMiddleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			// Risk assessment logic would go here
			// For now, just pass through
			return next(ctx, params)
		}
	}
}

// RecoveryMiddleware recovers from panics in tool handlers
func RecoveryMiddleware(logger func(format string, args ...any)) ToolMiddleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, params json.RawMessage) (result json.RawMessage, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered: %v", r)
					if logger != nil {
						logger("panic in tool handler: %v", r)
					}
				}
			}()

			return next(ctx, params)
		}
	}
}

// TimeoutMiddleware enforces timeout on tool execution
func TimeoutMiddleware(timeout time.Duration) ToolMiddleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			type result struct {
				data json.RawMessage
				err  error
			}

			ch := make(chan result, 1)
			go func() {
				data, err := next(ctx, params)
				ch <- result{data, err}
			}()

			select {
			case res := <-ch:
				return res.data, res.err
			case <-ctx.Done():
				return nil, fmt.Errorf("tool execution timeout after %v", timeout)
			}
		}
	}
}

// MetricsMiddleware collects metrics about tool execution
type Metrics struct {
	TotalCalls   int64
	TotalErrors  int64
	TotalLatency time.Duration
}

func MetricsMiddleware(metrics *Metrics) ToolMiddleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			start := time.Now()
			metrics.TotalCalls++

			result, err := next(ctx, params)

			metrics.TotalLatency += time.Since(start)
			if err != nil {
				metrics.TotalErrors++
			}

			return result, err
		}
	}
}