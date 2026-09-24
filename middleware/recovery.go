// Package middleware provides enterprise MCP middleware for the official Go SDK.
//
// Each middleware conforms to the [mcp.Middleware] signature and can be
// installed on a server via [mcp.Server.AddReceivingMiddleware].
//
// Middleware is applied right-to-left: AddReceivingMiddleware(m1, m2, m3)
// produces m1(m2(m3(handler))).
package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Recovery returns a middleware that catches panics in downstream handlers,
// logs a structured error via the provided logger, and returns a JSON-RPC
// internal error to the caller. It never re-panics.
func Recovery(logger *slog.Logger) mcp.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					stack := string(buf[:n])

					logger.ErrorContext(ctx, "panic recovered in method handler",
						slog.String("method", method),
						slog.String("panic", fmt.Sprint(r)),
						slog.String("stack", stack),
					)

					result = nil
					err = &jsonrpc.Error{
						Code:    jsonrpc.CodeInternalError,
						Message: fmt.Sprintf("internal error: panic in %s", method),
					}
				}
			}()
			return next(ctx, method, req)
		}
	}
}
