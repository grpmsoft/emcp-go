// Package client provides stdio transport for MCP protocol compatibility
package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// StdioTransport implements stdio-based transport for MCP client
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader
	mu     sync.Mutex
}

// NewStdioTransport creates a stdio transport that launches a subprocess
func NewStdioTransport(command string, args ...string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Redirect stderr to our stderr for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

// NewStdioTransportWithStreams creates a stdio transport with existing streams
// Useful for testing or custom stream handling
func NewStdioTransportWithStreams(stdin io.WriteCloser, stdout io.ReadCloser) *StdioTransport {
	return &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}
}

// Send writes a JSON-RPC message to stdin
func (t *StdioTransport) Send(msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Write message followed by newline
	if _, err := t.stdin.Write(msg); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if _, err := t.stdin.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// Receive reads a JSON-RPC message from stdout
func (t *StdioTransport) Receive() ([]byte, error) {
	// Read one line (blocking)
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("connection closed")
		}
		return nil, fmt.Errorf("failed to read message: %w", err)
	}

	return line, nil
}

// Close terminates the transport and subprocess
func (t *StdioTransport) Close() error {
	// Close stdin first to signal server to shutdown
	if t.stdin != nil {
		t.stdin.Close()
	}

	// Wait for process to exit (if we have one)
	if t.cmd != nil && t.cmd.Process != nil {
		if err := t.cmd.Wait(); err != nil {
			// Process may have already exited, that's ok
			_ = err
		}
	}

	// Close stdout
	if t.stdout != nil {
		t.stdout.Close()
	}

	return nil
}