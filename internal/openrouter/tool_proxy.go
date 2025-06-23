package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ToolProxy struct {
	proxyURL   string
	apiKey     string
	httpClient *http.Client
	tools      []Tool
}

func NewToolProxy(proxyURL, apiKey string) *ToolProxy {
	return &ToolProxy{
		proxyURL:   proxyURL,
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

func (tp *ToolProxy) LoadTools(ctx context.Context) error {
	// Fetch OpenAPI spec from mcp-compose proxy
	url := fmt.Sprintf("%s/openapi.json", tp.proxyURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if tp.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tp.apiKey)
	}

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var openAPISpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openAPISpec); err != nil {
		return err
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

	// Handle different argument types
	switch v := arguments.(type) {
	case map[string]interface{}:
		args = v
	case string:
		// Try to parse string as JSON
		if err := json.Unmarshal([]byte(v), &args); err != nil {
			return "", fmt.Errorf("failed to parse string arguments as JSON: %w", err)
		}
	case []byte:
		// Try to parse bytes as JSON
		if err := json.Unmarshal(v, &args); err != nil {
			return "", fmt.Errorf("failed to parse byte arguments as JSON: %w", err)
		}
	case json.RawMessage:
		// Handle json.RawMessage
		if err := json.Unmarshal(v, &args); err != nil {
			return "", fmt.Errorf("failed to parse RawMessage arguments as JSON: %w", err)
		}
	default:
		// Try to marshal and unmarshal to normalize
		if jsonBytes, err := json.Marshal(arguments); err != nil {
			return "", fmt.Errorf("failed to marshal arguments: %w", err)
		} else if err := json.Unmarshal(jsonBytes, &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
		}
	}

	// Call the mcp-compose proxy tool endpoint
	url := fmt.Sprintf("%s/%s", tp.proxyURL, toolName)

	requestBody, err := json.Marshal(args)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	if tp.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tp.apiKey)
	}

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("tool call failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Convert result to string representation
	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(resultBytes), nil
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
			if !ok {
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

	// Resolve $ref if present
	if _, hasRef := schemaMap["$ref"]; hasRef {
		// For now, return a generic schema for $ref
		// In a full implementation, you'd resolve the reference
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		}
	}

	return schemaMap
}
