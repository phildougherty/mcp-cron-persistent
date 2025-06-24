package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type ChatResponse struct {
	Id      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int     `json:"index"`
		FinishReason string  `json:"finish_reason"`
		Message      Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *OpenRouterError `json:"error,omitempty"`
}

// Custom error type to handle both string and number codes
type OpenRouterError struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Code    interface{} `json:"code"` // Can be string or number
}

func (e *OpenRouterError) GetCode() string {
	switch v := e.Code.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func NewClient(apiKey string, logger *logging.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) ExecuteAITaskWithTools(ctx context.Context, prompt, model string, tools []Tool, toolProxy *ToolProxy) (string, error) {
	taskId := ctx.Value("task_id")
	if taskId == nil {
		taskId = "unknown"
	}

	c.logger.Infof("[task_id=%v] Starting OpenRouter execution with %d tools available", taskId, len(tools))

	messages := []Message{
		{Role: "user", Content: prompt},
	}

	maxIterations := 30 // Increased for complex tasks
	for i := 0; i < maxIterations; i++ {
		c.logger.Debugf("[task_id=%v] OpenRouter iteration %d/%d", taskId, i+1, maxIterations)

		req := ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		}

		if len(tools) > 0 {
			req.ToolChoice = "auto"
		}

		c.logger.Debugf("[task_id=%v] Sending request to OpenRouter API", taskId)
		resp, err := c.sendRequest(ctx, req)
		if err != nil {
			c.logger.Errorf("[task_id=%v] OpenRouter API request failed: %v", taskId, err)
			return "", err
		}

		c.logger.Debugf("[task_id=%v] Received response from OpenRouter API", taskId)

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
			c.logger.Infof("[task_id=%v] OpenRouter task completed after %d iterations", taskId, i+1)
			return message.Content, nil
		}

		c.logger.Infof("[task_id=%v] OpenRouter requesting %d tool calls", taskId, len(message.ToolCalls))

		// Execute tool calls
		for _, toolCall := range message.ToolCalls {
			c.logger.Debugf("[task_id=%v] Executing tool: %s", taskId, toolCall.Function.Name)

			var args map[string]interface{}

			// Handle both string and object arguments from OpenRouter
			if len(toolCall.Function.Arguments) > 0 {
				// Try to parse as JSON object first
				if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
					// If that fails, try to parse as a JSON string containing JSON
					var argsString string
					if err2 := json.Unmarshal(toolCall.Function.Arguments, &argsString); err2 == nil {
						// Now parse the string as JSON
						if err3 := json.Unmarshal([]byte(argsString), &args); err3 != nil {
							c.logger.Errorf("[task_id=%v] Failed to parse tool arguments: %v. Raw: %s", taskId, err3, string(toolCall.Function.Arguments))
							args = map[string]interface{}{
								"error": fmt.Sprintf("Failed to parse arguments: %v", err3),
							}
						}
					} else {
						c.logger.Errorf("[task_id=%v] Failed to parse tool arguments as string or object: %v. Raw: %s", taskId, err, string(toolCall.Function.Arguments))
						args = map[string]interface{}{
							"error": fmt.Sprintf("Failed to parse arguments: %v", err),
						}
					}
				}
			}

			result, err := toolProxy.ExecuteTool(ctx, toolCall.Function.Name, args)
			if err != nil {
				result = fmt.Sprintf("Error executing tool %s: %v", toolCall.Function.Name, err)
				c.logger.Errorf("[task_id=%v] Tool execution failed for %s: %v", taskId, toolCall.Function.Name, err)
			} else {
				c.logger.Debugf("[task_id=%v] Tool %s executed successfully", taskId, toolCall.Function.Name)
			}

			// Add tool result to conversation
			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallId: toolCall.Id,
			})
		}
	}

	c.logger.Warnf("[task_id=%v] OpenRouter reached max iterations (%d) without completion", taskId, maxIterations)
	return "", fmt.Errorf("max iterations (%d) reached without completion", maxIterations)
}

func (c *Client) sendRequest(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/phildougherty/mcp-cron-persistent")
	httpReq.Header.Set("X-Title", "MCP-Cron Scheduler")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the raw response body first
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Debug log the raw response if there's an error status
	if resp.StatusCode >= 400 {
		taskId := ctx.Value("task_id")
		if taskId == nil {
			taskId = "unknown"
		}
		c.logger.Errorf("[task_id=%v] OpenRouter API error (status %d): %s", taskId, resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response (status %d): %w. Body: %s", resp.StatusCode, err, string(respBody))
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("OpenRouter API error: %s (type: %s, code: %s)",
			chatResp.Error.Message,
			chatResp.Error.Type,
			chatResp.Error.GetCode())
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return &chatResp, nil
}
