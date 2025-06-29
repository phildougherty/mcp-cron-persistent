// internal/server/dependency_tools.go - Make sure this file exists with all the parameter structs

package server

import (
	"fmt"
	"os"
	"time"

	"mcp-cron-persistent/internal/errors"
	"mcp-cron-persistent/internal/model"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
)

// DependencyTaskParams holds parameters for dependency task creation
type DependencyTaskParams struct {
	// Flatten the parameters instead of nesting AITaskParams
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
	Prompt      string `json:"prompt,omitempty" description:"prompt to use for AI"`

	DependsOn []string `json:"dependsOn" description:"task IDs this task depends on"`
}

// WatcherTaskParams holds parameters for watcher task creation
type WatcherTaskParams struct {
	// Flatten the parameters instead of nesting AITaskParams
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
	Prompt      string `json:"prompt,omitempty" description:"prompt to use for AI"`

	WatcherConfig *model.WatcherConfig `json:"watcherConfig" description:"watcher configuration"`
}

func (s *MCPServer) handleAddDependencyTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params DependencyTaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate basic task parameters
	if params.Type == model.TypeAI.String() {
		if err := validateAITaskParams(params.Name, params.Schedule, params.Prompt); err != nil {
			return createErrorResponse(err)
		}
	} else {
		if err := validateShellTaskParams(params.Name, params.Schedule, params.Command); err != nil {
			return createErrorResponse(err)
		}
	}

	if len(params.DependsOn) == 0 {
		return createErrorResponse(errors.InvalidInput("dependsOn is required for dependency tasks"))
	}

	s.logger.Debugf("Handling add_dependency_task request for task %s", params.Name)

	// Validate that dependency tasks exist
	for _, depID := range params.DependsOn {
		if _, err := s.scheduler.GetTask(depID); err != nil {
			return createErrorResponse(errors.InvalidInput("dependency task does not exist: " + depID))
		}
	}

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = params.Type
	task.Command = params.Command
	task.Prompt = params.Prompt
	task.DependsOn = params.DependsOn
	task.TriggerType = model.TriggerTypeDependency

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

func (s *MCPServer) handleAddWatcherTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params WatcherTaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate basic task parameters
	if params.Name == "" {
		return createErrorResponse(errors.InvalidInput("name is required"))
	}

	// Set default type if not specified
	if params.Type == "" {
		if params.Prompt != "" {
			params.Type = model.TypeAI.String()
		} else if params.Command != "" {
			params.Type = model.TypeShellCommand.String()
		} else {
			params.Type = model.TypeAI.String() // Default to AI
		}
	}

	// Validate based on type
	if params.Type == model.TypeAI.String() {
		if params.Prompt == "" {
			return createErrorResponse(errors.InvalidInput("prompt is required for AI watcher tasks"))
		}
	} else {
		if params.Command == "" {
			return createErrorResponse(errors.InvalidInput("command is required for shell watcher tasks"))
		}
	}

	// Validate watcher config
	if params.WatcherConfig == nil {
		return createErrorResponse(errors.InvalidInput("watcherConfig is required for watcher tasks"))
	}

	// Enhanced watcher config validation with defaults
	if err := validateAndSetWatcherDefaults(params.WatcherConfig); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_watcher_task request for task %s", params.Name)

	// Create task with proper defaults
	task := createBaseTask(params.Name, "", params.Description, params.Enabled)
	task.Type = params.Type
	task.Command = params.Command
	task.Prompt = params.Prompt
	task.WatcherConfig = params.WatcherConfig
	task.TriggerType = model.TriggerTypeWatcher
	task.Schedule = "" // Watchers don't use cron schedules

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

func validateAndSetWatcherDefaults(config *model.WatcherConfig) error {
	if config.Type == "" {
		return errors.InvalidInput("watcher type is required")
	}

	// Set default check interval if not specified
	if config.CheckInterval == "" {
		config.CheckInterval = "30s"
	}

	// Validate check interval format
	if _, err := time.ParseDuration(config.CheckInterval); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid checkInterval format: %s", config.CheckInterval))
	}

	// Set default TriggerOnce if not specified
	// (Go zero value for bool is false, which is often what we want)

	switch config.Type {
	case model.WatcherTypeFileCreation, model.WatcherTypeFileChange:
		if config.WatchPath == "" {
			return errors.InvalidInput("watchPath is required for file watchers")
		}
		// Validate path exists (warning only)
		if _, err := os.Stat(config.WatchPath); os.IsNotExist(err) {
			// Log warning but don't fail - path might be created later
			fmt.Printf("Warning: watchPath %s does not exist yet\n", config.WatchPath)
		}

	case model.WatcherTypeTaskCompletion:
		if len(config.WatchTaskIDs) == 0 {
			return errors.InvalidInput("watchTaskIDs is required for task completion watchers")
		}
		// Could validate that task IDs exist, but they might be created later

	default:
		return errors.InvalidInput(fmt.Sprintf("invalid watcher type: %s. Must be one of: %s, %s, %s",
			config.Type,
			model.WatcherTypeFileCreation,
			model.WatcherTypeFileChange,
			model.WatcherTypeTaskCompletion))
	}

	return nil
}
