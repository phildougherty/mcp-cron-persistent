// internal/server/dependency_tools.go - Make sure this file exists with all the parameter structs

package server

import (
	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/model"
)

// DependencyTaskParams holds parameters for dependency task creation
type DependencyTaskParams struct {
	AITaskParams
	DependsOn []string `json:"dependsOn" description:"task IDs this task depends on"`
}

// WatcherTaskParams holds parameters for watcher task creation
type WatcherTaskParams struct {
	AITaskParams
	WatcherConfig *model.WatcherConfig `json:"watcherConfig" description:"watcher configuration"`
}

// handleAddDependencyTask adds a task with dependencies
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

// handleAddWatcherTask adds a watcher task
func (s *MCPServer) handleAddWatcherTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params WatcherTaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate basic task parameters
	if params.Type == model.TypeAI.String() {
		if err := validateAITaskParams(params.Name, "", params.Prompt); err != nil { // No schedule for watchers
			return createErrorResponse(err)
		}
	} else {
		if err := validateTaskParams(params.Name, ""); err != nil { // No schedule for watchers
			return createErrorResponse(err)
		}
		if params.Command == "" {
			return createErrorResponse(errors.InvalidInput("command is required for shell watcher tasks"))
		}
	}

	if params.WatcherConfig == nil {
		return createErrorResponse(errors.InvalidInput("watcherConfig is required for watcher tasks"))
	}

	// Validate watcher config
	if err := validateWatcherConfig(params.WatcherConfig); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_watcher_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = params.Type
	task.Command = params.Command
	task.Prompt = params.Prompt
	task.WatcherConfig = params.WatcherConfig
	task.TriggerType = model.TriggerTypeWatcher

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// validateWatcherConfig validates watcher configuration
func validateWatcherConfig(config *model.WatcherConfig) error {
	if config.Type == "" {
		return errors.InvalidInput("watcher type is required")
	}

	switch config.Type {
	case model.WatcherTypeFileCreation, model.WatcherTypeFileChange:
		if config.WatchPath == "" {
			return errors.InvalidInput("watchPath is required for file watchers")
		}
	case model.WatcherTypeTaskCompletion:
		if len(config.WatchTaskIDs) == 0 {
			return errors.InvalidInput("watchTaskIDs is required for task completion watchers")
		}
	default:
		return errors.InvalidInput("invalid watcher type: " + config.Type)
	}

	// Set default check interval if not specified
	if config.CheckInterval == "" {
		config.CheckInterval = "30s"
	}

	return nil
}
