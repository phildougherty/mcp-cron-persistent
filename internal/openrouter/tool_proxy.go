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
		proxyURL: strings.TrimSuffix(proxyURL, "/"),
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (tp *ToolProxy) LoadTools(ctx context.Context) error {
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

	tp.tools = tp.convertOpenAPIToTools(openAPISpec)
	return nil
}

func (tp *ToolProxy) GetTools() []Tool {
	return tp.tools
}

func (tp *ToolProxy) LoadToolsFromServers(ctx context.Context, serverNames []string) error {
	var allTools []Tool

	for _, serverName := range serverNames {
		url := fmt.Sprintf("%s/%s", tp.proxyURL, serverName)

		reqBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"params":  map[string]interface{}{},
			"id":      1,
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request for server %s: %w", serverName, err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create request for server %s: %w", serverName, err)
		}

		req.Header.Set("Content-Type", "application/json")
		if tp.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+tp.apiKey)
		}

		resp, err := tp.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch tools from server %s: %w", serverName, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server %s returned status %d: %s", serverName, resp.StatusCode, string(bodyBytes))
		}

		var result struct {
			Result struct {
				Tools []map[string]interface{} `json:"tools"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to decode response from server %s: %w", serverName, err)
		}

		for _, mcpTool := range result.Result.Tools {
			name, _ := mcpTool["name"].(string)
			desc, _ := mcpTool["description"].(string)
			inputSchema, _ := mcpTool["inputSchema"].(map[string]interface{})

			if name != "" {
				fullToolName := fmt.Sprintf("mcp_%s_%s", serverName, name)

				tool := Tool{
					Type: "function",
					Function: Function{
						Name:        fullToolName,
						Description: desc,
						Parameters:  inputSchema,
					},
				}
				allTools = append(allTools, tool)
			}
		}
	}

	tp.tools = allTools
	return nil
}

func (tp *ToolProxy) ExecuteTool(ctx context.Context, toolName string, arguments interface{}) (string, error) {
	var args map[string]interface{}

	// Handle different argument types
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
		if jsonBytes, err := json.Marshal(arguments); err != nil {
			return "", fmt.Errorf("failed to marshal arguments of type %T: %w", arguments, err)
		} else if err := json.Unmarshal(jsonBytes, &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal normalized arguments: %w", err)
		}
	}

	// Parse tool name: mcp_{serverName}_{toolName}
	parts := strings.SplitN(toolName, "_", 3)
	if len(parts) < 3 || parts[0] != "mcp" {
		return "", fmt.Errorf("invalid tool name format: %s (expected mcp_SERVER_TOOL)", toolName)
	}

	serverName := parts[1]
	actualToolName := parts[2]

	// Make JSON-RPC request to server endpoint
	url := fmt.Sprintf("%s/%s", tp.proxyURL, serverName)

	rpcRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      actualToolName,
			"arguments": args,
		},
		"id": 1,
	}

	requestBody, err := json.Marshal(rpcRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if tp.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tp.apiKey)
	}

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tool call to %s/%s failed with status %d: %s", serverName, actualToolName, resp.StatusCode, string(bodyBytes))
	}

	// Parse JSON-RPC response
	var rpcResponse struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(bodyBytes, &rpcResponse); err != nil {
		// If JSON-RPC parsing fails, return raw response
		return string(bodyBytes), nil
	}

	if rpcResponse.Error != nil {
		return "", fmt.Errorf("MCP tool error: %s (code %d)", rpcResponse.Error.Message, rpcResponse.Error.Code)
	}

	// Extract text content from response
	if len(rpcResponse.Result.Content) > 0 {
		return rpcResponse.Result.Content[0].Text, nil
	}

	return "Success", nil
}

func (tp *ToolProxy) convertOpenAPIToTools(spec map[string]interface{}) []Tool {
	var tools []Tool

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		return tools
	}

	for _, pathSpec := range paths {
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
			if !ok || toolName == "" {
				continue
			}

			description := ""
			if desc, ok := postMap["description"].(string); ok {
				description = desc
			} else if summary, ok := postMap["summary"].(string); ok {
				description = summary
			} else {
				description = fmt.Sprintf("Tool: %s", toolName)
			}

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

	if _, hasRef := schemaMap["$ref"]; hasRef {
		return map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"required":             []string{},
			"additionalProperties": true,
		}
	}

	return schemaMap
}
