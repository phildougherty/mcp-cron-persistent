// SPDX-License-Identifier: AGPL-3.0-only
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jolks/mcp-cron/internal/logging"
)

type ToolProxy struct {
	proxyURL   string
	apiKey     string
	httpClient *http.Client
	tools      []Tool
	logger     *logging.Logger
}

func NewToolProxy(proxyURL, apiKey string, logger *logging.Logger) *ToolProxy {
	return &ToolProxy{
		proxyURL: proxyURL,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		tools:  []Tool{},
		logger: logger,
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
	tp.logger.Debugf("Loaded %d tools from OpenAPI spec", len(tp.tools))

	return nil
}

func (tp *ToolProxy) GetTools() []Tool {
	return tp.tools
}

func (tp *ToolProxy) ExecuteTool(ctx context.Context, toolName string, arguments json.RawMessage) (string, error) {
	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	tp.logger.Debugf("Executing tool %s with args: %v", toolName, args)

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
		return "", fmt.Errorf("tool request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the raw response body
	var responseBody bytes.Buffer
	if _, err := responseBody.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	responseStr := responseBody.String()
	tp.logger.Debugf("Tool %s raw response: %s", toolName, responseStr)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tool returned status %d: %s", resp.StatusCode, responseStr)
	}

	// Try to parse as JSON for pretty formatting, but if it fails, return the raw string
	var result interface{}
	if err := json.Unmarshal(responseBody.Bytes(), &result); err != nil {
		// If not valid JSON, return as-is
		tp.logger.Debugf("Tool %s returned non-JSON response, returning raw string", toolName)
		return responseStr, nil
	}

	// Convert result to formatted JSON string
	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		// Fallback to raw response if formatting fails
		return responseStr, nil
	}

	return string(resultBytes), nil
}

func (tp *ToolProxy) convertOpenAPIToTools(spec map[string]interface{}) []Tool {
	var tools []Tool

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		tp.logger.Warnf("No paths found in OpenAPI spec")
		return tools
	}

	for path, pathSpec := range paths {
		pathMap, ok := pathSpec.(map[string]interface{})
		if !ok {
			continue
		}

		if post, exists := pathMap["post"]; exists {
			postMap, ok := post.(map[string]interface{})
			if !ok {
				continue
			}

			operationId, ok := postMap["operationId"].(string)
			if !ok {
				tp.logger.Debugf("Skipping path %s: no operationId", path)
				continue
			}

			description, _ := postMap["description"].(string)
			if description == "" {
				description = fmt.Sprintf("Tool: %s", operationId)
			}

			// Extract parameters from requestBody schema
			parameters := tp.extractParametersFromRequestBody(postMap["requestBody"])

			tool := Tool{
				Type: "function",
				Function: Function{
					Name:        operationId,
					Description: description,
					Parameters:  parameters,
				},
			}

			tools = append(tools, tool)
			tp.logger.Debugf("Added tool: %s", operationId)
		}
	}

	return tools
}

func (tp *ToolProxy) extractParametersFromRequestBody(requestBody interface{}) map[string]interface{} {
	// Default schema
	defaultSchema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	if requestBody == nil {
		return defaultSchema
	}

	requestBodyMap, ok := requestBody.(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	content, ok := requestBodyMap["content"].(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	jsonContent, ok := content["application/json"].(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	schema, ok := jsonContent["schema"].(map[string]interface{})
	if !ok {
		return defaultSchema
	}

	return schema
}
