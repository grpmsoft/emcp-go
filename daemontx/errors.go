// Package daemontx provides an MCP Transport that connects to a daemon process
// discovered via PID file. It bridges the official MCP SDK's Transport interface
// with daemon-based service discovery and bearer token authentication.
package daemontx

import "errors"

// Sentinel errors for daemon discovery and transport.
var (
	// ErrDaemonNotRunning is returned when the PID file does not exist,
	// indicating the daemon process has not been started.
	ErrDaemonNotRunning = errors.New("daemon not running: PID file not found")

	// ErrPIDFileInvalid is returned when the PID file exists but cannot
	// be parsed as valid JSON or is missing required fields.
	ErrPIDFileInvalid = errors.New("PID file invalid")

	// ErrPortZero is returned when the PID file contains a zero port,
	// which means the daemon is not listening for HTTP connections.
	ErrPortZero = errors.New("daemon port is zero")
)
