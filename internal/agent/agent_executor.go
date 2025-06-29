// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/model"
)

// AgentExecutor handles executing commands with an agent
type AgentExecutor struct {
	mu      sync.Mutex
	results map[string]*model.Result // Map of taskID -> Result
	config  *config.Config
	// We'll add agent-specific fields here
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(cfg *config.Config) *AgentExecutor {
	return &AgentExecutor{
		results: make(map[string]*model.Result),
		config:  cfg,
	}
}

// Execute implements the Task execution for the scheduler
func (ae *AgentExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Runtime validation only checks fields needed for execution (ID and Prompt)
	// Schedule is validated at the API level but not required here because:
	// - The scheduler has already used the schedule to determine when to run the task
	// - Execution only needs the task ID and the content to execute
	if task.ID == "" || task.Prompt == "" {
		return fmt.Errorf("invalid task: missing ID or Prompt")
	}

	// Execute the command
	result := ae.ExecuteAgentTask(ctx, task.ID, task.Prompt, timeout, task)
	if result.Error != "" {
		return fmt.Errorf(result.Error)
	}

	return nil
}

// ExecuteAgentTask executes a command using an AI agent
func (ae *AgentExecutor) ExecuteAgentTask(
	ctx context.Context,
	taskID string,
	prompt string,
	timeout time.Duration,
	task *model.Task,
) *model.Result {
	result := &model.Result{
		Prompt:    prompt,
		StartTime: time.Now(),
		TaskID:    taskID,
	}

	// Store the result
	ae.mu.Lock()
	ae.results[taskID] = result
	ae.mu.Unlock()

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the task using RunTask
	output, err := RunTask(execCtx, task, ae.config)

	// Update result fields
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	if task != nil && task.ConversationID != "" {
		result.ConversationID = task.ConversationID
	}

	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 1
		result.Output = fmt.Sprintf("Error executing AI task: %v", err)
	} else {
		result.Output = output
		result.ExitCode = 0
	}

	// Convert the result to JSON and log it
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		errorJSON, _ := json.Marshal(map[string]string{
			"error":   "marshaling_error",
			"message": err.Error(),
			"task_id": taskID,
		})
		log.Println(string(errorJSON))
	} else {
		log.Println(string(jsonData))
	}

	return result
}

// GetTaskResult implements the ResultProvider interface
func (ae *AgentExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	result, exists := ae.results[taskID]
	return result, exists
}

func (ae *AgentExecutor) StoreResult(taskID string, result *model.Result) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.results[taskID] = result
}
