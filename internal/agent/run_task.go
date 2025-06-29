// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"

	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
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
