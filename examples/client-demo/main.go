// Package main demonstrates eMCP client usage
package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/goco-ai/emcp-go/client"
	"github.com/goco-ai/emcp-go/emcp"
)

var (
	serverCmd  = flag.String("server", "", "Path to MCP server executable")
	toolName   = flag.String("tool", "", "Tool name to call")
	args       = flag.String("args", "{}", "Tool arguments as JSON")
	listTools  = flag.Bool("list", false, "List available tools")
	jsonOutput = flag.Bool("json", false, "Output results as JSON")
)

func main() {
	flag.Parse()

	if *serverCmd == "" {
		fmt.Println("Usage: client-demo -server <path-to-server> [options]")
		fmt.Println("\nOptions:")
		fmt.Println("  -list           List available tools")
		fmt.Println("  -tool <name>    Call a specific tool")
		fmt.Println("  -args <json>    Tool arguments as JSON")
		fmt.Println("  -json           Output results as JSON")
		fmt.Println("\nExamples:")
		fmt.Println("  # List tools")
		fmt.Println("  client-demo -server ./basic.exe -list")
		fmt.Println()
		fmt.Println("  # Call echo tool")
		fmt.Println("  client-demo -server ./basic.exe -tool echo -args '{\"message\":\"Hello!\"}'")
		fmt.Println()
		fmt.Println("  # Call add tool")
		fmt.Println("  client-demo -server ./basic.exe -tool add -args '{\"a\":5,\"b\":3}'")
		fmt.Println()
		fmt.Println("  # JSON output (like Claude Code)")
		fmt.Println("  client-demo -server ./basic.exe -tool echo -args '{\"message\":\"Test\"}' -json")
		os.Exit(1)
	}

	// Create stdio transport
	transport, err := client.NewStdioTransport(*serverCmd)
	if err != nil {
		log.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	// Create client
	c := client.New(
		transport,
		client.WithClientInfo("emcp-client-demo", emcp.Version),
	)
	defer c.Close()

	ctx := context.Background()

	// Initialize connection
	if err := c.Initialize(ctx); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	serverInfo := c.ServerInfo()
	if !*jsonOutput {
		fmt.Printf("✓ Connected to %s v%s\n", serverInfo.Name, serverInfo.Version)
		fmt.Printf("  Protocol: %s\n\n", serverInfo.ProtocolVersion)
	}

	// List tools
	if *listTools {
		tools, err := c.ListTools(ctx)
		if err != nil {
			log.Fatalf("Failed to list tools: %v", err)
		}

		if *jsonOutput {
			outputJSON(map[string]any{
				"server": serverInfo,
				"tools":  tools,
			})
		} else {
			fmt.Printf("Available tools (%d):\n\n", len(tools))
			for _, tool := range tools {
				fmt.Printf("📦 %s\n", tool.Name)
				if tool.Description != "" {
					fmt.Printf("   %s\n", tool.Description)
				}
				fmt.Printf("   Risk: %s\n", tool.RiskLevel)
				if tool.InputSchema != nil {
					fmt.Printf("   Schema: %s\n", tool.InputSchema.Type)
					if len(tool.InputSchema.Properties) > 0 {
						fmt.Printf("   Parameters:\n")
						for name, prop := range tool.InputSchema.Properties {
							required := ""
							for _, r := range tool.InputSchema.Required {
								if r == name {
									required = " (required)"
									break
								}
							}
							fmt.Printf("     - %s: %s%s", name, prop.Type, required)
							if prop.Description != "" {
								fmt.Printf(" - %s", prop.Description)
							}
							fmt.Println()
						}
					}
				}
				fmt.Println()
			}
		}
		return
	}

	// Call tool
	if *toolName != "" {
		var argsData any
		if err := json.Unmarshal([]byte(*args), &argsData); err != nil {
			log.Fatalf("Invalid arguments JSON: %v", err)
		}

		if !*jsonOutput {
			fmt.Printf("⚡ Calling tool: %s\n", *toolName)
			fmt.Printf("   Arguments: %s\n\n", *args)
		}

		result, err := c.CallTool(ctx, *toolName, argsData)
		if err != nil {
			if *jsonOutput {
				outputJSON(map[string]any{
					"error": err.Error(),
				})
			} else {
				log.Fatalf("Tool call failed: %v", err)
			}
			return
		}

		if *jsonOutput {
			// Parse result to make it pretty
			var resultData any
			if err := json.Unmarshal(result, &resultData); err == nil {
				outputJSON(map[string]any{
					"tool":   *toolName,
					"result": resultData,
				})
			} else {
				outputJSON(map[string]any{
					"tool":   *toolName,
					"result": string(result),
				})
			}
		} else {
			fmt.Println("✓ Result:")
			// Pretty print JSON
			var prettyResult any
			if err := json.Unmarshal(result, &prettyResult); err == nil {
				prettyJSON, _ := json.Marshal(prettyResult)
				fmt.Printf("  %s\n", string(prettyJSON))
			} else {
				fmt.Printf("  %s\n", string(result))
			}
		}
		return
	}

	// Ping test
	if !*jsonOutput {
		fmt.Println("🏓 Testing connection with ping...")
	}
	if err := c.Ping(ctx); err != nil {
		if *jsonOutput {
			outputJSON(map[string]any{
				"ping":  "failed",
				"error": err.Error(),
			})
		} else {
			log.Fatalf("Ping failed: %v", err)
		}
		return
	}

	if *jsonOutput {
		outputJSON(map[string]any{
			"ping":   "success",
			"server": serverInfo,
		})
	} else {
		fmt.Println("✓ Ping successful!")
	}
}

func outputJSON(data any) {
	result, _ := json.Marshal(data)
	os.Stdout.Write(result)
	os.Stdout.Write([]byte("\n"))
}
