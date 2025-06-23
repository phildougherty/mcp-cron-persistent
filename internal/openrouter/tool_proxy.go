package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ToolProxy struct {
	proxyURL   string
	apiKey     string
	httpClient *http.Client
	tools      []Tool
}

func NewToolProxy(proxyURL, apiKey string) *ToolProxy {
	return &ToolProxy{
		proxyURL: strings.TrimSuffix(proxyURL, "/"), // Ensure no trailing slash
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Production timeout
		},
	}
}

func (tp *ToolProxy) LoadTools(ctx context.Context) error {
	// Fetch OpenAPI spec from mcp-compose proxy
	url := fmt.Sprintf("%s/openapi.json", tp.proxyURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for OpenAPI spec: %w", err)
	}

	if tp.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tp.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch OpenAPI spec from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("OpenAPI spec request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var openAPISpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openAPISpec); err != nil {
		return fmt.Errorf("failed to decode OpenAPI spec: %w", err)
	}

	// Convert OpenAPI spec to OpenRouter tool format
	tp.tools = tp.convertOpenAPIToTools(openAPISpec)
	return nil
}

func (tp *ToolProxy) GetTools() []Tool {
	return tp.tools
}

func (tp *ToolProxy) ExecuteTool(ctx context.Context, toolName string, arguments interface{}) (string, error) {
	var args map[string]interface{}

	// Handle different argument types with comprehensive error handling
	switch v := arguments.(type) {
	case map[string]interface{}:
		args = v
	case string:
		if v == "" {
			args = map[string]interface{}{}
		} else if err := json.Unmarshal([]byte(v), &args); err != nil {
			return "", fmt.Errorf("failed to parse string arguments as JSON: %w", err)
		}
	case []byte:
		if len(v) == 0 {
			args = map[string]interface{}{}
		} else if err := json.Unmarshal(v, &args); err != nil {
			return "", fmt.Errorf("failed to parse byte arguments as JSON: %w", err)
		}
	case json.RawMessage:
		if len(v) == 0 {
			args = map[string]interface{}{}
		} else if err := json.Unmarshal(v, &args); err != nil {
			return "", fmt.Errorf("failed to parse RawMessage arguments as JSON: %w", err)
		}
	case nil:
		args = map[string]interface{}{}
	default:
		// Try to marshal and unmarshal to normalize
		if jsonBytes, err := json.Marshal(arguments); err != nil {
			return "", fmt.Errorf("failed to marshal arguments of type %T: %w", arguments, err)
		} else if err := json.Unmarshal(jsonBytes, &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal normalized arguments: %w", err)
		}
	}

	// Call the mcp-compose proxy tool endpoint
	url := fmt.Sprintf("%s/%s", tp.proxyURL, toolName)

	requestBody, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mcp-cron-openrouter/1.0")
	if tp.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tp.apiKey)
	}

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tool call to %s failed with status %d: %s", toolName, resp.StatusCode, string(bodyBytes))
	}

	// Parse the response
	var result interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			// If JSON parsing fails, return the raw response
			return tp.formatToolResponse(toolName, args, map[string]interface{}{
				"raw_response": string(bodyBytes),
				"note":         "Response was not valid JSON",
			}), nil
		}
	} else {
		result = map[string]interface{}{
			"message": "Operation completed successfully (empty response)",
		}
	}

	// Return formatted response
	return tp.formatToolResponse(toolName, args, result), nil
}

func (tp *ToolProxy) formatToolResponse(toolName string, args map[string]interface{}, result interface{}) string {
	// Create a structured response that's easier for LLMs to understand
	summary := tp.generateSummary(toolName, args, result)

	response := map[string]interface{}{
		"tool_name": toolName,
		"status":    "success",
		"summary":   summary,
		"result":    result,
	}

	// Add argument summary for complex operations
	if len(args) > 0 {
		response["arguments_used"] = tp.summarizeArguments(toolName, args)
	}

	// Convert to nicely formatted JSON
	responseBytes, _ := json.MarshalIndent(response, "", "  ")
	return string(responseBytes)
}

func (tp *ToolProxy) generateSummary(toolName string, args map[string]interface{}, result interface{}) string {
	switch toolName {
	case "get_current_glucose":
		if resultMap, ok := result.(map[string]interface{}); ok {
			if value, ok := resultMap["glucose"].(float64); ok {
				if trend, ok := resultMap["trend"].(string); ok {
					return fmt.Sprintf("Current glucose: %.0f mg/dL, trending %s", value, trend)
				}
				return fmt.Sprintf("Current glucose: %.0f mg/dL", value)
			}
		}
		return "Retrieved current glucose reading"

	case "get_glucose_history":
		hours := 6 // default
		if h, ok := args["hours"].(float64); ok {
			hours = int(h)
		}
		if resultMap, ok := result.(map[string]interface{}); ok {
			if readings, ok := resultMap["readings"].([]interface{}); ok {
				return fmt.Sprintf("Retrieved %d glucose readings from the last %d hours", len(readings), hours)
			}
		}
		return fmt.Sprintf("Retrieved glucose history for the last %d hours", hours)

	case "create_entities":
		if entities, ok := args["entities"].([]interface{}); ok {
			entityNames := make([]string, 0, len(entities))
			for _, entity := range entities {
				if entityMap, ok := entity.(map[string]interface{}); ok {
					if name, ok := entityMap["name"].(string); ok {
						entityNames = append(entityNames, name)
					}
				}
			}
			if len(entityNames) > 0 {
				if len(entityNames) <= 3 {
					return fmt.Sprintf("Created %d entities: %s", len(entityNames), strings.Join(entityNames, ", "))
				}
				return fmt.Sprintf("Created %d entities including: %s, and %d others",
					len(entityNames),
					strings.Join(entityNames[:2], ", "),
					len(entityNames)-2)
			}
			return fmt.Sprintf("Created %d entities", len(entities))
		}
		return "Created entities in knowledge graph"

	case "create_relations":
		if relations, ok := args["relations"].([]interface{}); ok {
			return fmt.Sprintf("Created %d relations in knowledge graph", len(relations))
		}
		return "Created relations in knowledge graph"

	case "read_graph":
		if resultMap, ok := result.(map[string]interface{}); ok {
			if entities, ok := resultMap["entities"].([]interface{}); ok {
				if relations, ok := resultMap["relations"].([]interface{}); ok {
					return fmt.Sprintf("Retrieved knowledge graph: %d entities, %d relations", len(entities), len(relations))
				}
				return fmt.Sprintf("Retrieved knowledge graph: %d entities", len(entities))
			}
		}
		return "Retrieved knowledge graph data"

	case "search_nodes":
		if query, ok := args["query"].(string); ok {
			if resultMap, ok := result.(map[string]interface{}); ok {
				if nodes, ok := resultMap["nodes"].([]interface{}); ok {
					return fmt.Sprintf("Found %d nodes matching '%s'", len(nodes), query)
				}
			}
			return fmt.Sprintf("Searched knowledge graph for '%s'", query)
		}
		return "Searched knowledge graph"

	case "add_task", "add_ai_task":
		if name, ok := args["name"].(string); ok {
			return fmt.Sprintf("Created new task: %s", name)
		}
		return "Created new scheduled task"

	case "run_task":
		if taskId, ok := args["id"].(string); ok {
			return fmt.Sprintf("Executed task: %s", taskId)
		}
		return "Executed scheduled task"

	case "list_tasks":
		if resultArray, ok := result.([]interface{}); ok {
			return fmt.Sprintf("Retrieved %d scheduled tasks", len(resultArray))
		}
		return "Retrieved list of scheduled tasks"

	case "search_web":
		if query, ok := args["query"].(string); ok {
			return fmt.Sprintf("Searched web for: %s", query)
		}
		return "Performed web search"

	default:
		// Generic summary for unknown tools
		if len(args) > 0 {
			argCount := len(args)
			return fmt.Sprintf("Executed %s with %d argument(s)", toolName, argCount)
		}
		return fmt.Sprintf("Executed %s successfully", toolName)
	}
}

func (tp *ToolProxy) summarizeArguments(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "get_glucose_history":
		if hours, ok := args["hours"].(float64); ok {
			return fmt.Sprintf("hours=%v", hours)
		}
	case "search_nodes", "search_web":
		if query, ok := args["query"].(string); ok {
			return fmt.Sprintf("query='%s'", query)
		}
	case "create_entities":
		if entities, ok := args["entities"].([]interface{}); ok {
			return fmt.Sprintf("%d entities", len(entities))
		}
	case "create_relations":
		if relations, ok := args["relations"].([]interface{}); ok {
			return fmt.Sprintf("%d relations", len(relations))
		}
	}

	// Generic argument summary
	argKeys := make([]string, 0, len(args))
	for key := range args {
		argKeys = append(argKeys, key)
	}
	if len(argKeys) <= 3 {
		return strings.Join(argKeys, ", ")
	}
	return fmt.Sprintf("%s, and %d others", strings.Join(argKeys[:2], ", "), len(argKeys)-2)
}

func (tp *ToolProxy) convertOpenAPIToTools(spec map[string]interface{}) []Tool {
	var tools []Tool

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		return tools
	}

	for pathKey, pathSpec := range paths {
		pathMap, ok := pathSpec.(map[string]interface{})
		if !ok {
			continue
		}

		if post, exists := pathMap["post"]; exists {
			postMap, ok := post.(map[string]interface{})
			if !ok {
				continue
			}

			operationId, hasOpId := postMap["operationId"]
			if !hasOpId {
				continue
			}

			toolName, ok := operationId.(string)
			if !ok {
				continue
			}

			// Skip if tool name is empty or invalid
			if toolName == "" {
				continue
			}

			description := ""
			if desc, ok := postMap["description"].(string); ok {
				description = desc
			} else if summary, ok := postMap["summary"].(string); ok {
				description = summary
			} else {
				description = fmt.Sprintf("Tool from MCP server: %s", toolName)
			}

			// Extract parameters from requestBody schema
			parameters := tp.extractParametersFromRequestBody(postMap)

			tools = append(tools, Tool{
				Type: "function",
				Function: Function{
					Name:        toolName,
					Description: description,
					Parameters:  parameters,
				},
			})
		}
	}

	return tools
}

func (tp *ToolProxy) extractParametersFromRequestBody(postSpec map[string]interface{}) map[string]interface{} {
	// Default empty schema
	defaultSchema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	requestBody, exists := postSpec["requestBody"]
	if !exists {
		return defaultSchema
	}

	requestBodyMap, ok := requestBody.(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	content, exists := requestBodyMap["content"]
	if !exists {
		return defaultSchema
	}

	contentMap, ok := content.(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	jsonContent, exists := contentMap["application/json"]
	if !exists {
		return defaultSchema
	}

	jsonContentMap, ok := jsonContent.(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	schema, exists := jsonContentMap["schema"]
	if !exists {
		return defaultSchema
	}

	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	// Handle $ref resolution
	if ref, hasRef := schemaMap["$ref"]; hasRef {
		// Basic $ref handling - in production you might want to resolve these fully
		if refStr, ok := ref.(string); ok && strings.Contains(refStr, "ValidationError") {
			return defaultSchema
		}
		// Return a permissive schema for unknown $refs
		return map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"required":             []string{},
			"additionalProperties": true,
		}
	}

	return schemaMap
}
