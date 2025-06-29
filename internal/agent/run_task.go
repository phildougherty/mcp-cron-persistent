// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
	"mcp-cron-persistent/internal/model_router"
	"mcp-cron-persistent/internal/openrouter"
	"mcp-cron-persistent/internal/openwebui"
)

// RunTask executes an AI task using either OpenRouter or OpenWebUI
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)

	if cfg.UseOpenRouter || cfg.OpenRouter.Enabled {
		return runTaskWithOpenRouter(ctx, t, cfg, logger)
	} else {
		return runTaskWithOpenWebUI(ctx, t, cfg, logger)
	}
}

func runTaskWithOpenRouter(ctx context.Context, t *model.Task, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Infof("Running AI task: %s via OpenRouter", t.Name)

	// Create OpenRouter client
	client := openrouter.NewClient(cfg.OpenRouter.APIKey, logger)

	// Create tool proxy
	toolProxy := openrouter.NewToolProxy(cfg.OpenRouter.MCPProxyURL, cfg.OpenRouter.MCPProxyKey)

	// Load available tools
	if err := toolProxy.LoadTools(ctx); err != nil {
		logger.Errorf("Failed to load tools: %v", err)
		return "", err
	}

	tools := toolProxy.GetTools()
	logger.Infof("Loaded %d tools from MCP proxy", len(tools))

	// Execute task with tools
	result, err := client.ExecuteAITaskWithTools(ctx, t.Prompt, cfg.OpenRouter.Model, tools, toolProxy)
	if err != nil {
		logger.Errorf("Failed to execute AI task via OpenRouter: %v", err)
		return "", err
	}

	logger.Infof("AI task completed successfully via OpenRouter")
	return result, nil
}

func runTaskWithOpenWebUI(ctx context.Context, t *model.Task, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Infof("Running AI task: %s via OpenWebUI", t.Name)

	if !cfg.OpenWebUI.Enabled {
		return "", fmt.Errorf("OpenWebUI integration is disabled")
	}

	// Create OpenWebUI client
	client := openwebui.NewClient(&cfg.OpenWebUI, logger)

	// Execute the AI task via OpenWebUI
	result, err := client.ExecuteAITask(
		ctx,
		t.ID,
		t.Prompt,
		cfg.OpenWebUI.Model,
		cfg.OpenWebUI.UserID,
	)
	if err != nil {
		logger.Errorf("Failed to execute AI task via OpenWebUI: %v", err)
		return "", err
	}

	logger.Infof("AI task completed successfully via OpenWebUI")
	return result, nil
}

func RunTaskWithModelRouting(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)

	// Initialize model router
	var gatewayURL string
	if cfg.UseOpenRouter || cfg.OpenRouter.Enabled {
		gatewayURL = cfg.OpenRouter.MCPProxyURL
	}

	router := model_router.NewModelRouter(gatewayURL)

	// Create task context for model selection
	taskCtx := model_router.TaskContext{
		Task:             t,
		Hint:             detectTaskHint(t.Prompt),
		RequiredFeatures: detectRequiredFeatures(t.Prompt),
		SecurityLevel:    model_router.SecurityLocal, // Prefer local for privacy
		Priority:         "balanced",
	}

	// Get OLLAMA URL from config or environment
	ollamaURL := getOllamaURL(cfg)

	// Select appropriate model
	selection, err := router.SelectModelWithOllama(taskCtx, ollamaURL)
	if err != nil {
		logger.Errorf("Model selection failed: %v", err)
		// Fallback to original routing
		return RunTask(ctx, t, cfg)
	}

	logger.Infof("Selected model: %s (score: %.2f) - %s",
		selection.Model.Name, selection.Score, selection.Reasoning)

	// Route execution based on selected model
	if strings.HasPrefix(selection.Model.Name, "local/") {
		return executeWithOllama(ctx, t, selection, router, ollamaURL, logger)
	} else {
		return executeWithOpenRouter(ctx, t, selection, cfg, logger)
	}
}

func detectTaskHint(prompt string) string {
	prompt = strings.ToLower(prompt)

	switch {
	case strings.Contains(prompt, "quick") || strings.Contains(prompt, "fast"):
		return "fast"
	case strings.Contains(prompt, "cheap") || strings.Contains(prompt, "simple"):
		return "cheap"
	case strings.Contains(prompt, "complex") || strings.Contains(prompt, "detailed"):
		return "powerful"
	case strings.Contains(prompt, "private") || strings.Contains(prompt, "confidential"):
		return "local"
	default:
		return "balanced"
	}
}

func detectRequiredFeatures(prompt string) []string {
	prompt = strings.ToLower(prompt)
	features := []string{}

	if strings.Contains(prompt, "tool") || strings.Contains(prompt, "function") ||
		strings.Contains(prompt, "api") || strings.Contains(prompt, "execute") {
		features = append(features, "tool_use")
	}

	if strings.Contains(prompt, "image") || strings.Contains(prompt, "picture") ||
		strings.Contains(prompt, "photo") || strings.Contains(prompt, "visual") {
		features = append(features, "vision")
	}

	return features
}

func getOllamaURL(_ *config.Config) string {
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		return url
	}
	return "http://localhost:11434"
}

func executeWithOllama(ctx context.Context, t *model.Task, selection *model_router.ModelSelection, router *model_router.ModelRouter, ollamaURL string, logger *logging.Logger) (string, error) {
	logger.Infof("Executing task with OLLAMA model: %s", selection.Model.Name)

	messages := []model_router.OllamaChatMessage{
		{
			Role:    "user",
			Content: t.Prompt,
		},
	}

	resp, err := router.CreateOllamaCompletion(ctx, model_router.TaskContext{Task: t}, messages, ollamaURL)
	if err != nil {
		return "", fmt.Errorf("OLLAMA execution failed: %w", err)
	}

	return resp.Message.Content, nil
}

func executeWithOpenRouter(ctx context.Context, t *model.Task, selection *model_router.ModelSelection, cfg *config.Config, logger *logging.Logger) (string, error) {
	logger.Infof("Executing task with OpenRouter model: %s", selection.Model.Name)

	// Use the existing OpenRouter logic
	if cfg.UseOpenRouter || cfg.OpenRouter.Enabled {
		return runTaskWithOpenRouter(ctx, t, cfg, logger)
	} else {
		return runTaskWithOpenWebUI(ctx, t, cfg, logger)
	}
}
