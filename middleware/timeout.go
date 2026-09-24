package middleware

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Timeout returns a middleware that applies per-tool context deadlines
// to "tools/call" requests.
//
// The toolTimeouts map associates tool names with their maximum durations.
// Tools not present in the map use defaultTimeout. A zero defaultTimeout
// means no timeout is applied for unmapped tools.
//
// Non-tool-call methods pass through without any timeout modification.
func Timeout(toolTimeouts map[string]time.Duration, defaultTimeout time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != MethodToolsCall {
				return next(ctx, method, req)
			}

			toolName := extractToolName(req)

			timeout, ok := toolTimeouts[toolName]
			if !ok {
				timeout = defaultTimeout
			}

			if timeout <= 0 {
				return next(ctx, method, req)
			}

			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			return next(ctx, method, req)
		}
	}
}
