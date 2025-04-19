package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/model"
)

// CommandExecutor handles executing commands
type CommandExecutor struct {
	mu      sync.Mutex
	results map[string]*model.Result // Map of taskID -> Result
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{
		results: make(map[string]*model.Result),
	}
}

// Execute implements the Task execution for the scheduler
func (ce *CommandExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	if task.ID == "" || task.Command == "" {
		return fmt.Errorf("invalid task: missing ID or Command")
	}

	// Execute the command
	result := ce.ExecuteCommand(ctx, task.ID, task.Command, timeout)
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	return nil
}

// ExecuteCommand executes a shell command with a timeout
func (ce *CommandExecutor) ExecuteCommand(ctx context.Context, taskID, command string, timeout time.Duration) *model.Result {
	// Create a cancellable context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Prepare the command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Create result object
	result := &model.Result{
		Command:   command,
		StartTime: time.Now(),
		TaskID:    taskID,
	}

	// Store the result
	ce.mu.Lock()
	ce.results[taskID] = result
	ce.mu.Unlock()

	// Execute the command
	err := cmd.Run()

	// Update result fields
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	result.Output = strings.TrimSpace(stdout.String() + "\n" + stderr.String())

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		result.Error = err.Error()
	} else {
		result.ExitCode = 0
	}

	// Convert the result to JSON
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

// GetTaskResult returns the result of a previously executed task
func (ce *CommandExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	result, exists := ce.results[taskID]
	return result, exists
}
