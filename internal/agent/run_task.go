// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/openwebui"
)

// RunTask executes an AI task using OpenWebUI with conversation support
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s via OpenWebUI", t.Name)

	if !cfg.OpenWebUI.Enabled {
		return "", fmt.Errorf("OpenWebUI integration is disabled")
	}

	// Create OpenWebUI client
	client := openwebui.NewClient(&cfg.OpenWebUI, logger)

	// Prepare the prompt with context and memory
	enhancedPrompt := buildEnhancedPrompt(t, cfg)

	// If this task doesn't have a conversation ID, create one if it's marked as an agent
	if t.ConversationID == "" && t.IsAgent {
		conversationName := t.ConversationName
		if conversationName == "" {
			conversationName = fmt.Sprintf("Agent: %s", t.Name)
		}

		conversation, err := client.CreateConversation(ctx, conversationName)
		if err != nil {
			logger.Warnf("Failed to create conversation for agent task: %v", err)
			// Fall back to standard execution
		} else {
			t.ConversationID = conversation.ID
			logger.Infof("Created conversation %s for agent task", conversation.ID)
		}
	}

	// Execute the AI task via OpenWebUI
	var result string
	var err error

	if t.ConversationID != "" {
		result, err = client.ExecuteAITaskWithConversation(
			ctx,
			t.ID,
			enhancedPrompt,
			cfg.OpenWebUI.Model,
			cfg.OpenWebUI.UserID,
			t.ConversationID,
		)
	} else {
		result, err = client.ExecuteAITask(
			ctx,
			t.ID,
			enhancedPrompt,
			cfg.OpenWebUI.Model,
			cfg.OpenWebUI.UserID,
		)
	}

	if err != nil {
		logger.Errorf("Failed to execute AI task via OpenWebUI: %v", err)
		return "", err
	}

	// Update memory summary if this is an agent task
	if t.IsAgent {
		updateTaskMemory(t, result)
	}

	logger.Infof("AI task completed successfully via OpenWebUI")
	return result, nil
}

// buildEnhancedPrompt creates an enhanced prompt with context and memory
func buildEnhancedPrompt(task *model.Task, _ *config.Config) string {
	prompt := task.Prompt

	// Add agent personality if available
	if task.IsAgent && task.AgentPersonality != "" {
		prompt = fmt.Sprintf("You are %s\n\n%s", task.AgentPersonality, prompt)
	}

	// Add memory context if available
	if task.MemorySummary != "" {
		memoryContext := fmt.Sprintf(`
Previous Memory Summary:
%s

Current Task:
%s`, task.MemorySummary, prompt)
		prompt = memoryContext
	}

	// Add task context
	if task.ConversationContext != "" {
		contextualPrompt := fmt.Sprintf(`
Context: %s

Task: %s`, task.ConversationContext, prompt)
		prompt = contextualPrompt
	}

	return prompt
}

// updateTaskMemory updates the task's memory summary based on the execution result
func updateTaskMemory(task *model.Task, result string) {
	// Simple memory update - in a production system, you might want to use
	// an LLM to intelligently summarize the important parts
	if len(result) > 500 {
		task.MemorySummary = result[:500] + "... (truncated)"
	} else {
		task.MemorySummary = result
	}

	now := time.Now()
	task.LastMemoryUpdate = &now
}
