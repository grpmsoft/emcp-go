// Package main demonstrates eMCP gRPC client-server communication.
// This example shows how to use eMCP with high-performance gRPC transport.
package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/goco-ai/emcp-go/client"
	"github.com/goco-ai/emcp-go/emcp"
	"github.com/goco-ai/emcp-go/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: grpc-demo <server|client>")
		os.Exit(1)
	}

	mode := os.Args[1]

	switch mode {
	case "server":
		runServer()
	case "client":
		runClient()
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		fmt.Println("Usage: grpc-demo <server|client>")
		os.Exit(1)
	}
}

// runServer starts eMCP gRPC server.
func runServer() {
	fmt.Println("🚀 Starting eMCP gRPC Server...")
	fmt.Println(strings.Repeat("=", 50))

	// Create server
	srv := server.New(
		"demo-server",
		"1.0.0",
		server.WithMiddleware(loggingMiddleware),
	)

	// Register example tools
	registerTools(srv)

	// Create gRPC transport
	transport := server.NewGRPCTransport(
		srv,
		":50051",
		server.WithVerbose(true),
	)

	fmt.Println("✅ Server listening on :50051")
	fmt.Println("📦 Registered tools: echo, uppercase, add")
	fmt.Println("\nPress Ctrl+C to stop...")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\n🛑 Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		transport.Shutdown(ctx)
		os.Exit(0)
	}()

	// Start server
	if err := transport.Serve(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// runClient demonstrates eMCP gRPC client.
func runClient() {
	fmt.Println("🔌 eMCP gRPC Client Demo")
	fmt.Println(strings.Repeat("=", 50))

	ctx := context.Background()

	// Connect to server
	fmt.Println("\n📡 Connecting to localhost:50051...")
	transport, err := client.NewGRPCTransport("localhost:50051")
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer transport.Close()

	fmt.Println("✅ Connected!")

	// Initialize
	fmt.Println("\n🤝 Initializing...")
	clientInfo := emcp.ClientInfo{
		Name:    "demo-client",
		Version: "1.0.0",
	}

	info, err := transport.Initialize(ctx, clientInfo)
	if err != nil {
		log.Fatalf("Initialize failed: %v", err)
	}

	fmt.Printf("✅ Connected to: %s v%s (MCP %s)\n", info.Name, info.Version, info.ProtocolVersion)

	// List tools
	fmt.Println("\n📋 Available tools:")
	tools, err := transport.ListTools(ctx)
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	for _, tool := range tools {
		fmt.Printf("  • %s - %s (risk: %s)\n", tool.Name, tool.Description, tool.RiskLevel)
	}

	// Call tools
	fmt.Println("\n🔧 Calling tools:")

	// Example 1: Echo
	fmt.Println("\n1. Echo tool:")
	result, err := transport.CallTool(ctx, "echo", map[string]any{
		"message": "Hello from gRPC!",
	})
	if err != nil {
		log.Printf("  ❌ Error: %v", err)
	} else {
		printResult(result)
	}

	// Example 2: Uppercase
	fmt.Println("\n2. Uppercase tool:")
	result, err = transport.CallTool(ctx, "uppercase", map[string]any{
		"text": "grpc is awesome",
	})
	if err != nil {
		log.Printf("  ❌ Error: %v", err)
	} else {
		printResult(result)
	}

	// Example 3: Add
	fmt.Println("\n3. Add tool:")
	result, err = transport.CallTool(ctx, "add", map[string]any{
		"a": 42,
		"b": 58,
	})
	if err != nil {
		log.Printf("  ❌ Error: %v", err)
	} else {
		printResult(result)
	}

	fmt.Println("\n✅ Demo completed successfully!")
}

// registerTools registers example tools on server.
func registerTools(srv *server.Server) {
	// Echo tool
	echoTool := emcp.ToolDefinition{
		Name:        "echo",
		Description: "Echoes back the input message",
		InputSchema: &emcp.JSONSchema{
			Type: "object",
			Properties: map[string]*emcp.SchemaProperty{
				"message": {
					Type:        "string",
					Description: "Message to echo",
				},
			},
			Required: []string{"message"},
		},
		RiskLevel: emcp.RiskLow,
	}

	srv.AddTool(echoTool, func(ctx context.Context, params []byte) ([]byte, error) {
		var input struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}

		result := map[string]string{"echo": input.Message}
		return json.Marshal(result)
	})

	// Uppercase tool
	uppercaseTool := emcp.ToolDefinition{
		Name:        "uppercase",
		Description: "Converts text to uppercase",
		InputSchema: &emcp.JSONSchema{
			Type: "object",
			Properties: map[string]*emcp.SchemaProperty{
				"text": {
					Type:        "string",
					Description: "Text to convert",
				},
			},
			Required: []string{"text"},
		},
		RiskLevel: emcp.RiskLow,
	}

	srv.AddTool(uppercaseTool, func(ctx context.Context, params []byte) ([]byte, error) {
		var input struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}

		result := map[string]string{"result": strings.ToUpper(input.Text)}
		return json.Marshal(result)
	})

	// Add tool
	addTool := emcp.ToolDefinition{
		Name:        "add",
		Description: "Adds two numbers",
		InputSchema: &emcp.JSONSchema{
			Type: "object",
			Properties: map[string]*emcp.SchemaProperty{
				"a": {
					Type:        "number",
					Description: "First number",
				},
				"b": {
					Type:        "number",
					Description: "Second number",
				},
			},
			Required: []string{"a", "b"},
		},
		RiskLevel: emcp.RiskLow,
	}

	srv.AddTool(addTool, func(ctx context.Context, params []byte) ([]byte, error) {
		var input struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}

		result := map[string]float64{"result": input.A + input.B}
		return json.Marshal(result)
	})
}

// loggingMiddleware logs tool calls.
func loggingMiddleware(next server.ToolHandler) server.ToolHandler {
	return func(ctx context.Context, params []byte) ([]byte, error) {
		start := time.Now()
		result, err := next(ctx, params)
		duration := time.Since(start)

		if err != nil {
			log.Printf("Tool failed after %v: %v", duration, err)
		} else {
			log.Printf("Tool succeeded in %v", duration)
		}

		return result, err
	}
}

// printResult pretty-prints tool result.
func printResult(data []byte) {
	fmt.Printf("  ✅ Result: %s\n", string(data))
}
