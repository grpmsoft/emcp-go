package client

import (
	"context"
	"testing"
	"time"
)

// TestStdioClientWithGODA tests direct stdio communication with GODA MCP server
func TestStdioClientWithGODA(t *testing.T) {
	godaPath := "../../goda/goda.exe"

	// Create stdio transport
	t.Log("📡 Creating stdio transport...")
	transport, err := NewStdioTransport(godaPath, "mcp", "serve", "--use-emcp", "--headless")
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	t.Log("✅ Stdio transport created")

	// Create client
	t.Log("🔧 Creating MCP client...")
	client := New(transport, WithClientInfo("test-client", "1.0.0"))
	defer client.Close()

	t.Log("✅ MCP client created")

	// Initialize with timeout
	t.Log("🔄 Initializing connection...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	initStart := time.Now()
	err = client.Initialize(ctx)
	initDuration := time.Since(initStart)

	if err != nil {
		t.Fatalf("❌ Initialize failed after %v: %v", initDuration, err)
	}

	t.Logf("✅ Initialize succeeded in %v", initDuration)

	// Check server info
	serverInfo := client.ServerInfo()
	if serverInfo == nil {
		t.Fatal("❌ Server info is nil")
	}

	t.Logf("✅ Server: %s v%s", serverInfo.Name, serverInfo.Version)

	// List tools
	t.Log("📋 Listing tools...")
	toolsCtx, toolsCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer toolsCancel()

	tools, err := client.ListTools(toolsCtx)
	if err != nil {
		t.Fatalf("❌ ListTools failed: %v", err)
	}

	t.Logf("✅ Found %d tools:", len(tools))
	for i, tool := range tools {
		t.Logf("   %d. %s - %s", i+1, tool.Name, tool.Description)
	}
}

// TestStdioTransportRaw tests raw stdio transport without client
func TestStdioTransportRaw(t *testing.T) {
	godaPath := "../../goda/goda.exe"

	// Create stdio transport
	t.Log("📡 Creating stdio transport...")
	transport, err := NewStdioTransport(godaPath, "mcp", "serve", "--use-emcp", "--headless")
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	t.Log("✅ Stdio transport created")

	// Send initialize request
	initRequest := `{"jsonrpc":"2.0","id":"1","method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test-client","version":"1.0.0"},"capabilities":{}}}`

	t.Log("📤 Sending initialize request...")
	err = transport.Send([]byte(initRequest))
	if err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	t.Log("✅ Request sent, waiting for response...")

	// Read response with timeout
	responseChan := make(chan []byte, 1)
	errorChan := make(chan error, 1)

	go func() {
		resp, err := transport.Receive()
		if err != nil {
			errorChan <- err
			return
		}
		responseChan <- resp
	}()

	select {
	case resp := <-responseChan:
		t.Logf("✅ Received response: %s", string(resp))

	case err := <-errorChan:
		t.Fatalf("❌ Receive error: %v", err)

	case <-time.After(15 * time.Second):
		t.Fatal("⚠️ TIMEOUT: No response after 15 seconds")
	}
}

// TestStdioTransportDebug adds extensive logging
func TestStdioTransportDebug(t *testing.T) {
	godaPath := "../../goda/goda.exe"

	t.Log("=== DEBUG TEST START ===")

	// Create stdio transport
	t.Log("Step 1: Creating stdio transport...")
	transport, err := NewStdioTransport(godaPath, "mcp", "serve", "--use-emcp", "--headless")
	if err != nil {
		t.Fatalf("FAILED at Step 1: %v", err)
	}
	defer transport.Close()

	t.Log("Step 1: SUCCESS - Transport created")

	// Send request
	initRequest := `{"jsonrpc":"2.0","id":"1","method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1.0"},"capabilities":{}}}`

	t.Logf("Step 2: Sending request (length: %d bytes)...", len(initRequest))
	t.Logf("Request: %s", initRequest)

	err = transport.Send([]byte(initRequest))
	if err != nil {
		t.Fatalf("FAILED at Step 2: %v", err)
	}

	t.Log("Step 2: SUCCESS - Request sent")

	// Try to receive
	t.Log("Step 3: Attempting to receive response...")
	t.Log("Step 3: Calling transport.Receive()...")

	// Create goroutine with logging
	responseChan := make(chan []byte, 1)
	errorChan := make(chan error, 1)

	go func() {
		t.Log("Step 3: [goroutine] Started")
		t.Log("Step 3: [goroutine] About to call Receive()...")

		resp, err := transport.Receive()

		if err != nil {
			t.Logf("Step 3: [goroutine] Receive returned ERROR: %v", err)
			errorChan <- err
			return
		}

		t.Logf("Step 3: [goroutine] Receive returned DATA: %d bytes", len(resp))
		responseChan <- resp
	}()

	// Wait with progress updates
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := time.After(20 * time.Second)
	elapsed := 0

	for {
		select {
		case resp := <-responseChan:
			t.Log("Step 3: SUCCESS - Response received!")
			t.Logf("Response: %s", string(resp))
			return

		case err := <-errorChan:
			t.Fatalf("Step 3: FAILED - Error: %v", err)

		case <-ticker.C:
			elapsed++
			t.Logf("Step 3: Waiting... (%d seconds elapsed)", elapsed)

		case <-timeout:
			t.Fatal("Step 3: TIMEOUT - No response after 20 seconds")
		}
	}
}
