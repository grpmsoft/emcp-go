package daemontx

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Compile-time check: DaemonTransport must satisfy mcp.Transport.
var _ mcp.Transport = (*DaemonTransport)(nil)

// DaemonTransport implements mcp.Transport by discovering a daemon process
// through its PID file and connecting via the official SDK's StreamableClientTransport.
//
// On Connect, it reads the PID file to obtain the daemon's port and bearer token,
// constructs the MCP endpoint URL, and delegates to StreamableClientTransport
// for the actual HTTP-based JSON-RPC connection.
type DaemonTransport struct {
	// PIDFilePath is the absolute path to the daemon's PID file.
	// Required. Connect returns an error if this is empty.
	PIDFilePath string

	// MCPPath is the HTTP path for the MCP endpoint on the daemon.
	// Defaults to "/mcp" if empty.
	MCPPath string

	// HTTPClient is the HTTP client used for MCP requests.
	// If nil, http.DefaultClient is used (with auth round-tripper injected).
	// When provided, the auth round-tripper wraps its existing Transport.
	HTTPClient *http.Client

	// DisableStandaloneSSE controls whether the client establishes a standalone
	// SSE stream for server-initiated messages. Defaults to true, which is
	// appropriate for plain JSON-RPC servers (like GODE) that do not support SSE.
	//
	// Set to false only if the daemon's MCP server supports and requires
	// server-sent events for notifications.
	DisableStandaloneSSE *bool
}

// Connect discovers the daemon via PID file and establishes an MCP connection.
//
// It reads the PID file to obtain port and token, constructs the endpoint URL
// (http://127.0.0.1:<port><mcpPath>), injects bearer token authentication
// into every outgoing request, and delegates to StreamableClientTransport.Connect.
func (d *DaemonTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	port, token, err := Discover(d.PIDFilePath)
	if err != nil {
		return nil, fmt.Errorf("daemon discovery failed: %w", err)
	}

	mcpPath := d.MCPPath
	if mcpPath == "" {
		mcpPath = "/mcp"
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", port, mcpPath)

	// Build HTTP client with auth round-tripper.
	httpClient := d.resolveHTTPClient(token)

	// Default DisableStandaloneSSE to true (daemon servers typically don't support SSE).
	disableSSE := true
	if d.DisableStandaloneSSE != nil {
		disableSSE = *d.DisableStandaloneSSE
	}

	inner := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: disableSSE,
	}

	return inner.Connect(ctx)
}

// resolveHTTPClient wraps the base HTTP client's transport with an
// authRoundTripper that injects the bearer token into every request.
func (d *DaemonTransport) resolveHTTPClient(token string) *http.Client {
	base := d.HTTPClient
	if base == nil {
		base = &http.Client{}
	}

	baseRT := base.Transport
	if baseRT == nil {
		baseRT = http.DefaultTransport
	}

	return &http.Client{
		Transport:     &authRoundTripper{base: baseRT, token: token},
		CheckRedirect: base.CheckRedirect,
		Jar:           base.Jar,
		Timeout:       base.Timeout,
	}
}

// authRoundTripper injects a Bearer token into the Authorization header
// of every outgoing HTTP request.
type authRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (a *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.token != "" {
		// Clone the request to avoid mutating the caller's original.
		r := req.Clone(req.Context())
		r.Header.Set("Authorization", "Bearer "+a.token)
		return a.base.RoundTrip(r)
	}
	return a.base.RoundTrip(req)
}
