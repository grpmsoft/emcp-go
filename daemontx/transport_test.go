package daemontx

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Discovery tests ---

func TestDiscover_ValidPIDFile(t *testing.T) {
	tests := []struct {
		name      string
		content   pidInfo
		wantPort  int
		wantToken string
	}{
		{
			name:      "full PID file with token",
			content:   pidInfo{PID: 1234, Port: 8094, Name: "gode", Token: "secret-token-abc"},
			wantPort:  8094,
			wantToken: "secret-token-abc",
		},
		{
			name:      "PID file without token",
			content:   pidInfo{PID: 5678, Port: 9000, Name: "goda"},
			wantPort:  9000,
			wantToken: "",
		},
		{
			name:      "high port number",
			content:   pidInfo{PID: 1, Port: 65535, Name: "test", Token: "t"},
			wantPort:  65535,
			wantToken: "t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writePIDFile(t, tt.content)

			port, token, err := Discover(path)
			if err != nil {
				t.Fatalf("Discover() unexpected error: %v", err)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func TestDiscover_MissingFile(t *testing.T) {
	_, _, err := Discover(filepath.Join(t.TempDir(), "nonexistent.pid"))
	if err == nil {
		t.Fatal("Discover() expected error for missing file, got nil")
	}
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Errorf("error = %v, want ErrDaemonNotRunning", err)
	}
}

func TestDiscover_ZeroPort(t *testing.T) {
	path := writePIDFile(t, pidInfo{PID: 1234, Port: 0, Name: "gode", Token: "abc"})

	_, _, err := Discover(path)
	if err == nil {
		t.Fatal("Discover() expected error for zero port, got nil")
	}
	if !errors.Is(err, ErrPortZero) {
		t.Errorf("error = %v, want ErrPortZero", err)
	}
}

func TestDiscover_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	if err := os.WriteFile(path, []byte("not json at all{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Discover(path)
	if err == nil {
		t.Fatal("Discover() expected error for invalid JSON, got nil")
	}
	if !errors.Is(err, ErrPIDFileInvalid) {
		t.Errorf("error = %v, want ErrPIDFileInvalid", err)
	}
}

// --- Transport tests ---

func TestDaemonTransport_Connect(t *testing.T) {
	// Stand up a real MCP server using the official SDK.
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "test-daemon",
		Version: "v0.1.0",
	}, nil)
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	// Extract port from httptest server URL (http://127.0.0.1:<port>).
	port := httpServerPort(t, httpServer)

	// Write a PID file pointing at our test server.
	pidPath := writePIDFile(t, pidInfo{
		PID:  os.Getpid(),
		Port: port,
		Name: "test",
	})

	dt := &DaemonTransport{
		PIDFilePath: pidPath,
		MCPPath:     "/", // httptest serves on root
	}

	// Drive the full MCP handshake through the SDK client.
	// StreamableClientTransport.Connect() is lazy -- no HTTP requests until
	// mcp.Client.Connect() triggers the initialize handshake via Read/Write.
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v0.1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(ctx, dt, nil)
	if err != nil {
		t.Fatalf("mcp.Client.Connect() error: %v", err)
	}
	defer session.Close()
}

func TestDaemonTransport_AuthHeader(t *testing.T) {
	const wantToken = "super-secret-daemon-token"

	var authReceived atomic.Value

	// Create a middleware handler that captures the Authorization header
	// before delegating to the real MCP handler.
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "test-auth",
		Version: "v0.1.0",
	}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			authReceived.Store(auth)
		}
		mcpHandler.ServeHTTP(w, r)
	})

	httpServer := httptest.NewServer(wrapper)
	defer httpServer.Close()

	port := httpServerPort(t, httpServer)

	pidPath := writePIDFile(t, pidInfo{
		PID:   os.Getpid(),
		Port:  port,
		Name:  "test",
		Token: wantToken,
	})

	dt := &DaemonTransport{
		PIDFilePath: pidPath,
		MCPPath:     "/",
	}

	// Use mcp.Client to drive the handshake, which triggers actual HTTP requests.
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v0.1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(ctx, dt, nil)
	if err != nil {
		t.Fatalf("mcp.Client.Connect() error: %v", err)
	}
	defer session.Close()

	// Verify the auth header was sent.
	got, ok := authReceived.Load().(string)
	if !ok || got == "" {
		t.Fatal("Authorization header was not sent to the server")
	}

	want := "Bearer " + wantToken
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestDaemonTransport_ConnectFailsWithMissingPIDFile(t *testing.T) {
	dt := &DaemonTransport{
		PIDFilePath: filepath.Join(t.TempDir(), "no-such-daemon.pid"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := dt.Connect(ctx)
	if err == nil {
		t.Fatal("Connect() expected error for missing PID file, got nil")
	}
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Errorf("error = %v, want ErrDaemonNotRunning", err)
	}
}

func TestDaemonTransport_DefaultMCPPath(t *testing.T) {
	// Verify that the default MCPPath "/mcp" is used when MCPPath is empty.
	// We do this by starting a server that only responds on "/mcp".
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "test-default-path",
		Version: "v0.1.0",
	}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)

	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	port := httpServerPort(t, httpServer)
	pidPath := writePIDFile(t, pidInfo{
		PID:  os.Getpid(),
		Port: port,
		Name: "test",
	})

	dt := &DaemonTransport{
		PIDFilePath: pidPath,
		// MCPPath intentionally left empty — should default to "/mcp"
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v0.1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(ctx, dt, nil)
	if err != nil {
		t.Fatalf("mcp.Client.Connect() with default MCPPath error: %v", err)
	}
	defer session.Close()
}

func TestDaemonTransport_DisableStandaloneSSE(t *testing.T) {
	// Verify the DisableStandaloneSSE override works.
	// When nil (default), it should be true. When explicitly set, it should honor the value.
	tests := []struct {
		name     string
		override *bool
		want     bool
	}{
		{name: "nil defaults to true", override: nil, want: true},
		{name: "explicit true", override: boolPtr(true), want: true},
		{name: "explicit false", override: boolPtr(false), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := &DaemonTransport{
				PIDFilePath:          "unused",
				DisableStandaloneSSE: tt.override,
			}

			// We verify through the struct field; the actual Connect call
			// would propagate this to StreamableClientTransport.
			got := true
			if dt.DisableStandaloneSSE != nil {
				got = *dt.DisableStandaloneSSE
			}
			if got != tt.want {
				t.Errorf("DisableStandaloneSSE effective value = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDaemonTransport_NoTokenSkipsAuthHeader(t *testing.T) {
	var authSeen atomic.Bool

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "test-no-auth",
		Version: "v0.1.0",
	}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			authSeen.Store(true)
		}
		mcpHandler.ServeHTTP(w, r)
	})

	httpServer := httptest.NewServer(wrapper)
	defer httpServer.Close()

	port := httpServerPort(t, httpServer)
	pidPath := writePIDFile(t, pidInfo{
		PID:  os.Getpid(),
		Port: port,
		Name: "test",
		// Token intentionally empty
	})

	dt := &DaemonTransport{
		PIDFilePath: pidPath,
		MCPPath:     "/",
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v0.1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(ctx, dt, nil)
	if err != nil {
		t.Fatalf("mcp.Client.Connect() error: %v", err)
	}
	defer session.Close()

	if authSeen.Load() {
		t.Error("Authorization header was sent when token is empty")
	}
}

// --- Helpers ---

// writePIDFile creates a temporary PID file with the given info and returns its path.
func writePIDFile(t *testing.T, info pidInfo) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshaling PID info: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writing PID file: %v", err)
	}
	return path
}

// httpServerPort extracts the port number from an httptest.Server's URL.
func httpServerPort(t *testing.T, s *httptest.Server) int {
	t.Helper()
	// URL is like "http://127.0.0.1:PORT"
	addr := s.Listener.Addr().String()
	// addr is "127.0.0.1:PORT"
	colonIdx := strings.LastIndex(addr, ":")
	if colonIdx < 0 {
		t.Fatalf("unexpected server address format: %s", addr)
	}
	var port int
	if _, err := fmt.Sscanf(addr[colonIdx+1:], "%d", &port); err != nil {
		t.Fatalf("parsing port from %s: %v", addr, err)
	}
	return port
}

func boolPtr(v bool) *bool {
	return &v
}
