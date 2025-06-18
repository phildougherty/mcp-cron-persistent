// SPDX-License-Identifier: AGPL-3.0-only
package openwebui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
)

// ChatRequest represents a chat request to OpenWebUI
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse represents a chat response from OpenWebUI
type ChatResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Client represents an OpenWebUI API client
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *logging.Logger
}

// NewClient creates a new OpenWebUI client
func NewClient(cfg *config.OpenWebUIConfig, logger *logging.Logger) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeout) * time.Second,
		},
		logger: logger,
	}
}

// ExecuteAITask executes an AI task via OpenWebUI
func (c *Client) ExecuteAITask(ctx context.Context, taskID, prompt, model, userID string) (string, error) {
	c.logger.Debugf("Executing AI task %s via OpenWebUI", taskID)

	// Prepare the request - use the model if specified, otherwise let OpenWebUI use default
	request := ChatRequest{
		Model: model,
		Messages: []Message{
			{
				Role: "system",
				Content: fmt.Sprintf(`You are executing a scheduled AI task.
Task ID: %s
Please execute the following request and provide a complete response.
This is an automated task execution from the mcp-cron scheduler.`, taskID),
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	// Marshal request to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request - use the correct endpoint
	endpoint := c.baseURL + "/api/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	c.logger.Debugf("Sending request to OpenWebUI: %s", endpoint)

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenWebUI API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenWebUI API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var chatResponse ChatResponse
	if err := json.Unmarshal(body, &chatResponse); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResponse.Choices) == 0 {
		return "", fmt.Errorf("OpenWebUI returned no response choices")
	}

	result := chatResponse.Choices[0].Message.Content
	c.logger.Infof("AI task %s completed successfully via OpenWebUI", taskID)

	return result, nil
}

// Health checks if OpenWebUI is accessible
func (c *Client) Health(ctx context.Context) error {
	endpoint := c.baseURL + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call OpenWebUI health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenWebUI health check failed with status: %d", resp.StatusCode)
	}

	return nil
}

// GetModels retrieves available models from OpenWebUI
func (c *Client) GetModels(ctx context.Context) ([]string, error) {
	endpoint := c.baseURL + "/api/models"
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get models from OpenWebUI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenWebUI models API returned status: %d", resp.StatusCode)
	}

	// Parse the models response
	var modelsResp struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	var models []string
	for _, model := range modelsResp.Data {
		models = append(models, model.ID)
	}

	return models, nil
}
