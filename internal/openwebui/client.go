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
	Model    string    `json:"model,omitempty"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	UserID   string    `json:"user_id,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse represents a chat response from OpenWebUI
type ChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// StreamedChatResponse represents a streamed chat response
type StreamedChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
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

	// Prepare the request
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
		UserID: userID,
	}

	// Marshal request to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	endpoint := c.baseURL + "/api/chat"
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

	result := chatResponse.Message.Content
	c.logger.Infof("AI task %s completed successfully via OpenWebUI", taskID)

	return result, nil
}

// ExecuteAITaskStreamed executes an AI task with streaming response
func (c *Client) ExecuteAITaskStreamed(ctx context.Context, taskID, prompt, model, userID string) (string, error) {
	c.logger.Debugf("Executing AI task %s via OpenWebUI (streamed)", taskID)

	// Prepare the request
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
		Stream: true,
		UserID: userID,
	}

	// Marshal request to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	endpoint := c.baseURL + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	c.logger.Debugf("Sending streamed request to OpenWebUI: %s", endpoint)

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenWebUI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenWebUI API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Read streaming response
	var fullResponse strings.Builder
	decoder := json.NewDecoder(resp.Body)

	for {
		var streamResponse StreamedChatResponse
		if err := decoder.Decode(&streamResponse); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to decode streaming response: %w", err)
		}

		fullResponse.WriteString(streamResponse.Message.Content)

		if streamResponse.Done {
			break
		}
	}

	result := fullResponse.String()
	c.logger.Infof("AI task %s completed successfully via OpenWebUI (streamed)", taskID)

	return result, nil
}

// Health checks if OpenWebUI is accessible
func (c *Client) Health(ctx context.Context) error {
	endpoint := c.baseURL + "/api/models"
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
