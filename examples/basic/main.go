// Package main demonstrates basic eMCP server usage
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/goco-ai/emcp-go/emcp"
	"github.com/goco-ai/emcp-go/server"
)

func main() {
	// Create server
	srv := server.New(
		"example-server",
		"0.1.0",
		server.WithMiddleware(server.LoggingMiddleware(log.Printf)),
		server.WithInitHook(func(ctx context.Context, clientInfo emcp.ClientInfo) error {
			log.Printf("Client connected: %s v%s", clientInfo.Name, clientInfo.Version)
			return nil
		}),
	)

	// Register a simple echo tool
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

	err := srv.AddTool(echoTool, func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		var input struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		result := map[string]string{
			"echo": input.Message,
		}

		return json.Marshal(result)
	})
	if err != nil {
		log.Fatalf("Failed to add echo tool: %v", err)
	}

	// Register a calculator tool
	calcTool := emcp.ToolDefinition{
		Name:        "add",
		Description: "Adds two numbers",
		InputSchema: &emcp.JSONSchema{
			Type: "object",
			Properties: map[string]*emcp.SchemaProperty{
				"a": {Type: "number", Description: "First number"},
				"b": {Type: "number", Description: "Second number"},
			},
			Required: []string{"a", "b"},
		},
		RiskLevel: emcp.RiskLow,
	}

	err = srv.AddTool(calcTool, func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		var input struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		result := map[string]float64{
			"result": input.A + input.B,
		}

		return json.Marshal(result)
	})
	if err != nil {
		log.Fatalf("Failed to add calculator tool: %v", err)
	}

	// Setup stdio transport
	transport := server.NewStdioTransport(srv)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5)
		defer cancel()
		transport.Shutdown(ctx)
		os.Exit(0)
	}()

	// Start server
	log.Println("eMCP server starting on stdio...")
	if err := transport.Serve(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}