// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ThinkInAIXYZ/go-mcp/client"
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/openai/openai-go"
)

type toolCaller func(context.Context, openai.ChatCompletionMessageToolCall) (string, error)

func buildToolsFromConfig() ([]openai.ChatCompletionToolParam, toolCaller, error) {
	// Parse the config file
	// TODO: support Env
	var cfg struct {
		MCP map[string]struct {
			Command string   `json:"command,omitempty"`
			Args    []string `json:"args,omitempty"`
			URL     string   `json:"url,omitempty"`
		} `json:"mcpServers"`
	}
	// TODO: read from env var or default to ~/.cursor/mcp.json
	raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".cursor", "mcp.json"))
	if err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, err
	}

	// Create a go-mcp client per server and collect its tools
	var tools []openai.ChatCompletionToolParam
	cliBySrv := map[string]*client.Client{}
	tool2srv := map[string]string{} // toolName -> serverName

	for name, spec := range cfg.MCP {
		var tp transport.ClientTransport
		switch {
		case spec.Command != "":
			tp, err = transport.NewStdioClientTransport(spec.Command, spec.Args)
		case spec.URL != "":
			tp, err = transport.NewSSEClientTransport(spec.URL)
		default:
			continue
		}
		if err != nil {
			// Log and continue. Don't abort discovery
			log.Printf("Failed to create transport for server %s: %v\n", name, err)
			continue
		}

		cli, err := client.NewClient(tp)
		if err != nil {
			log.Printf("Failed to create client for server %s: %v\n", name, err)
			continue
		}
		cliBySrv[name] = cli

		resp, err := cli.ListTools(context.Background())
		if err != nil {
			log.Printf("Failed to list tools for server %s: %v\n", name, err)
			continue
		}
		for _, tl := range resp.Tools {
			// Extract the raw JSON‑schema
			var rawSchema []byte
			if tl.RawInputSchema != nil {
				rawSchema = tl.RawInputSchema
			} else {
				if b, err := json.Marshal(tl.InputSchema); err == nil {
					rawSchema = b
				} else {
					log.Printf("Failed to marshal input schema for tool %s: %v\n", tl.Name, err)
					continue
				}
			}
			// Unmarshal into map[string]interface{} for the SDK
			var params map[string]interface{}
			if err := json.Unmarshal(rawSchema, &params); err != nil {
				log.Printf("Failed to unmarshal input schema for tool %s: %v\n", tl.Name, err)
				continue
			}

			// WORKAROUND: Fix empty parameter schemas to avoid OpenAI API errors
			// Check if this is an empty schema (no properties)
			if params["type"] == "object" && (params["properties"] == nil || len(params["properties"].(map[string]interface{})) == 0) {
				// Add a dummy property to satisfy OpenAI API requirements
				props := map[string]interface{}{
					"random_string": map[string]interface{}{
						"type":        "string",
						"description": "Dummy parameter for no-parameter tools",
					},
				}
				params["properties"] = props
				params["required"] = []string{"random_string"}
				log.Printf("Added dummy parameter to empty schema for tool %s\n", tl.Name)
			}

			tools = append(tools, openai.ChatCompletionToolParam{
				Function: openai.FunctionDefinitionParam{
					Name:        tl.Name,
					Description: openai.String(tl.Description),
					Parameters:  params,
				},
			})
			tool2srv[tl.Name] = name
		}
	}
	// log.Printf("Found tools: %v\n", tools)
	// No tools. Fallback to LLM
	if len(tools) == 0 {
		return nil, nil, nil
	}
	// Dispatcher to route model's tool calls to the correct MCP server
	dispatcher := func(ctx context.Context, call openai.ChatCompletionMessageToolCall) (string, error) {
		// Parse arguments JSON string into a map
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		// Check if tool name exists in mapping
		serverName, ok := tool2srv[call.Function.Name]
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
		}

		// Check if server exists in client mapping
		cli, ok := cliBySrv[serverName]
		if !ok {
			return "", fmt.Errorf("server not found for tool: %s", call.Function.Name)
		}

		req := protocol.NewCallToolRequest(call.Function.Name, args)
		res, err := cli.CallTool(ctx, req)
		if err != nil {
			return "", err
		}
		// Flatten the tool response into a single string
		out, _ := json.Marshal(res.Content)
		return string(out), nil
	}
	return tools, dispatcher, nil
}
