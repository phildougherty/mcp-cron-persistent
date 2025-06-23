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

type Client struct {
	apiKey     string
	httpClient *http.Client
	logger     *logging.Logger
}

type ChatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallId string     `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	Id       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func NewClient(apiKey string, logger *logging.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) ExecuteAITaskWithTools(ctx context.Context, prompt, model string, tools []Tool, toolProxy *ToolProxy) (string, error) {
	messages := []Message{
		{Role: "user", Content: prompt},
	}

	maxIterations := 10
	for i := 0; i < maxIterations; i++ {
		c.logger.Debugf("OpenRouter iteration %d/%d", i+1, maxIterations)

		req := ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		}

		if len(tools) > 0 {
			req.ToolChoice = "auto"
		}

		resp, err := c.sendRequest(ctx, req)
		if err != nil {
			return "", err
		}

		// Check for API errors
		if resp.Error != nil {
			return "", fmt.Errorf("OpenRouter API error: %s", resp.Error.Message)
		}

		// Process response
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("no response from OpenRouter")
		}

		choice := resp.Choices[0]
		message := choice.Message

		// Add assistant message to conversation
		messages = append(messages, message)

		// If no tool calls, we're done
		if len(message.ToolCalls) == 0 {
			c.logger.Infof("OpenRouter task completed after %d iterations", i+1)
			return message.Content, nil
		}

		c.logger.Infof("OpenRouter requesting %d tool calls", len(message.ToolCalls))

		// Execute tool calls
		for _, toolCall := range message.ToolCalls {
			c.logger.Debugf("Executing tool: %s", toolCall.Function.Name)
			result, err := toolProxy.ExecuteTool(ctx, toolCall.Function.Name, toolCall.Function.Arguments)
			if err != nil {
				c.logger.Errorf("Tool execution failed for %s: %v", toolCall.Function.Name, err)
				result = fmt.Sprintf("Error executing tool %s: %v", toolCall.Function.Name, err)
			} else {
				c.logger.Debugf("Tool %s executed successfully", toolCall.Function.Name)
			}

			// Add tool result to conversation
			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallId: toolCall.Id,
			})
		}
	}

	return "", fmt.Errorf("max iterations (%d) reached without completion", maxIterations)
}

func (c *Client) sendRequest(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	requestBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/jolks/mcp-cron")
	httpReq.Header.Set("X-Title", "MCP Cron Persistent")

	c.logger.Debugf("Sending request to OpenRouter API")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var chatResponse ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if chatResponse.Error != nil {
			return nil, fmt.Errorf("OpenRouter API error (status %d): %s", resp.StatusCode, chatResponse.Error.Message)
		}
		return nil, fmt.Errorf("OpenRouter API returned status %d", resp.StatusCode)
	}

	c.logger.Debugf("Received response from OpenRouter API")
	return &chatResponse, nil
}
