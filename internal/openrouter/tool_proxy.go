// SPDX-License-Identifier: AGPL-3.0-only
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch OpenAPI spec: status %d", resp.StatusCode)
	}

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

func (tp *ToolProxy) ExecuteTool(ctx context.Context, toolName string, arguments json.RawMessage) (string, error) {
	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
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

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tool execution failed with status %d", resp.StatusCode)
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

			toolName, ok := postMap["operationId"].(string)
			if !ok {
				continue
			}

			description := ""
			if desc, ok := postMap["description"].(string); ok {
				description = desc
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
	// Default empty parameters
	defaultParams := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	requestBody, exists := postSpec["requestBody"]
	if !exists {
		return defaultParams
	}

	requestBodyMap, ok := requestBody.(map[string]interface{})
	if !ok {
		return defaultParams
	}

	content, exists := requestBodyMap["content"]
	if !exists {
		return defaultParams
	}

	contentMap, ok := content.(map[string]interface{})
	if !ok {
		return defaultParams
	}

	applicationJson, exists := contentMap["application/json"]
	if !exists {
		return defaultParams
	}

	applicationJsonMap, ok := applicationJson.(map[string]interface{})
	if !ok {
		return defaultParams
	}

	schema, exists := applicationJsonMap["schema"]
	if !exists {
		return defaultParams
	}

	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return defaultParams
	}

	return schemaMap
}
