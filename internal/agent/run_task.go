// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OllamaRequest represents a request to Ollama API
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// OllamaResponse represents a response from Ollama API
type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// OpenRouterRequest represents a request to OpenRouter API
type OpenRouterRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenRouterResponse represents a response from OpenRouter API
type OpenRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// RunTask executes an AI task using the configured LLM provider
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s using provider: %s", t.Name, cfg.AI.Provider)

	// Add timeout to context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.AI.RequestTimeout)*time.Second)
	defer cancel()

	switch strings.ToLower(cfg.AI.Provider) {
	case "ollama":
		return callOllama(timeoutCtx, t.Prompt, cfg, logger)
	case "openrouter":
		return callOpenRouter(timeoutCtx, t.Prompt, cfg, logger)
	case "openai":
		return callOpenAI(timeoutCtx, t, cfg, logger)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", cfg.AI.Provider)
	}
}

// callOllama calls the Ollama API
func callOllama(ctx context.Context, prompt string, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Debugf("Calling Ollama at %s with model %s", cfg.AI.GetOllamaEndpoint(), cfg.AI.OllamaModel)

	request := OllamaRequest{
		Model:  cfg.AI.OllamaModel,
		Prompt: prompt,
		Stream: false,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Ollama request: %w", err)
	}

	endpoint := cfg.AI.GetOllamaEndpoint() + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama API returned status: %d", resp.StatusCode)
	}

	var response OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode Ollama response: %w", err)
	}

	logger.Infof("AI task completed successfully using Ollama")
	return response.Response, nil
}

// callOpenRouter calls the OpenRouter API
func callOpenRouter(ctx context.Context, prompt string, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Debugf("Calling OpenRouter with model %s", cfg.AI.OpenRouterModel)

	request := OpenRouterRequest{
		Model: cfg.AI.OpenRouterModel,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal OpenRouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.AI.OpenRouterURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create OpenRouter request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.AI.OpenRouterAPIKey)
	req.Header.Set("X-Title", "MCP-Cron AI Task")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenRouter API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		var errorBody bytes.Buffer
		errorBody.ReadFrom(resp.Body)
		return "", fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, errorBody.String())
	}

	var response OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode OpenRouter response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("OpenRouter returned no response choices")
	}

	logger.Infof("AI task completed successfully using OpenRouter")
	return response.Choices[0].Message.Content, nil
}

// callOpenAI calls the OpenAI API (legacy support)
func callOpenAI(ctx context.Context, t *model.Task, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Debugf("Calling OpenAI with model %s", cfg.AI.Model)

	// Get tools for the AI agent
	tools, dispatcher, err := buildToolsFromConfig(cfg)
	if err != nil {
		logger.Errorf("Failed to build tools: %v", err)
		return "", err
	}

	// Check for API key
	apiKey := cfg.AI.OpenAIAPIKey
	if apiKey == "" {
		logger.Errorf("OpenAI API key is not set in configuration")
		return "", fmt.Errorf("OpenAI API key is not set in configuration")
	}

	// Create OpenAI client
	client := openai.NewClient(option.WithAPIKey(apiKey))

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(t.Prompt),
	}

	// Fallback to LLM if no tools
	if len(tools) == 0 {
		logger.Infof("No tools available, using basic chat completion")
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    cfg.AI.Model,
			Messages: msgs,
		})
		if err != nil {
			logger.Errorf("Chat completion failed: %v", err)
			return "", err
		}
		result := resp.Choices[0].Message.Content
		logger.Infof("AI task completed successfully using OpenAI")
		return result, nil
	}

	// Tool-enabled loop
	maxIterations := cfg.AI.MaxToolIterations
	logger.Infof("Starting tool-enabled AI task with max %d iterations", maxIterations)

	for i := 0; i < maxIterations; i++ {
		logger.Debugf("AI task iteration %d", i+1)
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    cfg.AI.Model,
			Messages: msgs,
			Tools:    tools,
		})
		if err != nil {
			logger.Errorf("Chat completion failed on iteration %d: %v", i+1, err)
			return "", err
		}

		m := resp.Choices[0].Message

		// If no tool calls, return the content
		if len(m.ToolCalls) == 0 {
			logger.Infof("AI task completed successfully with %d iterations using OpenAI", i+1)
			return m.Content, nil
		}

		// Add the assistant message with tool calls to the conversation first
		msgs = append(msgs, m.ToParam())

		// Process tool calls
		logger.Debugf("Processing %d tool calls in iteration %d", len(m.ToolCalls), i+1)
		for j, call := range m.ToolCalls {
			logger.Debugf("Tool call %d: %s", j+1, call.Function.Name)
			out, err := dispatcher(ctx, call)
			if err != nil {
				logger.Warnf("Tool call error: %v", err)
				out = "ERROR: " + err.Error()
			}
			msgs = append(msgs, openai.ToolMessage(out, call.ID))
		}
	}

	logger.Errorf("AI task exceeded maximum iterations (%d)", maxIterations)
	return "", fmt.Errorf("tool loop exceeded maximum iterations (%d)", maxIterations)
}
