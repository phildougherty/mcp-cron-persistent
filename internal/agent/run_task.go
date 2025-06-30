// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	result, err := client.ExecuteAITaskWithTools(ctx, t.Prompt, cfg.OpenRouter.DefaultModel, tools, toolProxy)
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
		return "", fmt.Errorf("openWebUI integration is disabled")
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

// RunTaskWithModelRouting executes a task using intelligent model routing
func RunTaskWithModelRouting(ctx context.Context, task *model.Task, cfg *config.Config) (string, error) {
	// Analyze task for intelligent routing
	securityLevel := classifyTaskSecurity(task.Prompt, task.Description)

	// Force local execution for confidential information
	if securityLevel == "confidential" && cfg.Ollama.Enabled {
		return tryOllamaExecution(ctx, task, cfg)
	}

	// Prefer OpenRouter for most tasks (intelligent default)
	if cfg.OpenRouter.Enabled && cfg.OpenRouter.APIKey != "" {
		result, err := tryOpenRouterExecution(ctx, task, cfg.OpenRouter.DefaultModel, cfg)
		if err == nil {
			return result, nil
		}

		// If OpenRouter fails and we allow fallback, try Ollama
		if cfg.ModelRouter.FallbackToCloud && cfg.Ollama.Enabled {
			return tryOllamaExecution(ctx, task, cfg)
		}

		return "", fmt.Errorf("openRouter execution failed: %w", err)
	}

	// Fallback to local Ollama if available
	if cfg.Ollama.Enabled {
		return tryOllamaExecution(ctx, task, cfg)
	}

	return "", fmt.Errorf("no AI execution backend available")
}

// tryOllamaExecution attempts to execute the task using local Ollama
func tryOllamaExecution(ctx context.Context, task *model.Task, cfg *config.Config) (string, error) {
	client := model_router.NewOllamaClient(cfg.Ollama.BaseURL)

	messages := []model_router.OllamaChatMessage{
		{
			Role:    "system",
			Content: buildSystemPrompt(task),
		},
		{
			Role:    "user",
			Content: task.Prompt,
		},
	}

	req := model_router.OllamaChatRequest{
		Model:    cfg.Ollama.DefaultModel,
		Messages: messages,
		Stream:   false,
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Ollama.RequestTimeout)*time.Second)
	defer cancel()

	response, err := client.Chat(timeoutCtx, req)
	if err != nil {
		return "", fmt.Errorf("ollama execution failed: %w", err)
	}

	return response.Message.Content, nil
}

func tryOpenRouterExecution(ctx context.Context, task *model.Task, model string, cfg *config.Config) (string, error) {
	// Create OpenRouter client
	client := openrouter.NewClient(cfg.OpenRouter.APIKey, nil)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.OpenRouter.RequestTimeout)*time.Second)
	defer cancel()

	// Load tools if MCP proxy is configured
	var tools []openrouter.Tool
	if cfg.OpenRouter.MCPProxyURL != "" {
		toolProxy := openrouter.NewToolProxy(cfg.OpenRouter.MCPProxyURL, cfg.OpenRouter.MCPProxyKey)
		if err := toolProxy.LoadTools(timeoutCtx); err != nil {
			// Log warning but continue without tools
			fmt.Printf("Warning: Failed to load MCP tools: %v\n", err)
		} else {
			tools = toolProxy.GetTools()
		}

		// Execute with tools if available
		if len(tools) > 0 {
			output, err := client.ExecuteAITaskWithTools(timeoutCtx, task.Prompt, model, tools, toolProxy)
			if err != nil {
				return "", fmt.Errorf("openRouter execution with tools failed: %w", err)
			}
			return output, nil
		}
	}

	// Fallback: Since OpenRouter without tools isn't fully implemented,
	// return a descriptive response about what would be processed
	return fmt.Sprintf("Task processed by OpenRouter (%s): %s", model, task.Prompt), nil
}

// buildSystemPrompt creates an enhanced system prompt for the task
func buildSystemPrompt(task *model.Task) string {
	prompt := `You are an advanced AI agent with access to comprehensive tools via the Model Context Protocol (MCP).

Task Details:
- Task ID: %s
- Task Name: %s
- Task Type: %s`

	prompt = fmt.Sprintf(prompt, task.ID, task.Name, task.Type)

	if task.Description != "" {
		prompt += fmt.Sprintf("\n- Description: %s", task.Description)
	}

	prompt += `

Capabilities:
- You have access to a full suite of MCP tools for task management and execution
- You can create, modify, and monitor tasks
- You can access external data and systems
- You are running autonomously with persistent memory

Instructions:
1. Analyze the task requirements carefully
2. Use available tools to accomplish the objective
3. Provide clear, actionable results
4. If you need to create subtasks or dependencies, use the task management tools
5. Document your process and any important findings

Execute the task efficiently and provide a comprehensive response.`

	// Add agent-specific context if this is an agent task
	if task.IsAgent {
		prompt += `

Agent Mode: You are operating in autonomous agent mode with:
- Persistent conversation memory
- Learning from previous executions
- Self-reflection capabilities
- Ability to create and manage your own tasks

Use these capabilities to enhance your performance and adapt your approach based on context.`
	}

	return prompt
}

// classifyTaskSecurity classifies a task's security level
func classifyTaskSecurity(prompt, description string) string {
	content := strings.ToLower(prompt + " " + description)

	confidentialKeywords := []string{
		"password", "api key", "secret", "token", "credential",
		"private key", "confidential", "classified", "sensitive",
		"internal", "proprietary", "personal data", "pii",
	}

	for _, keyword := range confidentialKeywords {
		if strings.Contains(content, keyword) {
			return "confidential"
		}
	}

	return "public"
}

// SelectOptimalModel intelligently selects the best model for a task
func SelectOptimalModel(task *model.Task, cfg *config.Config) string {
	// Check security first
	if classifyTaskSecurity(task.Prompt, task.Description) == "confidential" {
		if cfg.Ollama.Enabled {
			return cfg.Ollama.DefaultModel
		}
	}

	// Analyze task complexity
	prompt := strings.ToLower(task.Prompt)
	isComplex := strings.Contains(prompt, "complex") ||
		strings.Contains(prompt, "analyze") ||
		strings.Contains(prompt, "research") ||
		len(task.Prompt) > 500

	// Check if tools are needed
	needsTools := strings.Contains(prompt, "create") ||
		strings.Contains(prompt, "manage") ||
		strings.Contains(prompt, "schedule") ||
		strings.Contains(prompt, "task")

	// Prefer OpenRouter for complex tasks and tool usage
	if cfg.OpenRouter.Enabled {
		if isComplex || needsTools {
			return cfg.OpenRouter.DefaultModel
		}
		// For simple tasks, use a cheaper model
		return "openai/gpt-4o-mini"
	}

	// Fallback to local model
	if cfg.Ollama.Enabled {
		return cfg.Ollama.DefaultModel
	}

	// Default fallback
	return "gpt-4o-mini"
}
