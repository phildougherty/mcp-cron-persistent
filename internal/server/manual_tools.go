// internal/server/manual_tools.go

package server

import (
	"fmt"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/model"
)

// handleAddManualTask adds a manual-only task
func (s *MCPServer) handleAddManualTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params AITaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters (no schedule needed for manual tasks)
	if params.Type == model.TypeAI.String() {
		if err := validateTaskParams(params.Name, "manual"); err != nil {
			return createErrorResponse(err)
		}
		if params.Prompt == "" {
			return createErrorResponse(errors.InvalidInput("prompt is required for AI tasks"))
		}
	} else {
		if err := validateTaskParams(params.Name, "manual"); err != nil {
			return createErrorResponse(err)
		}
		if params.Command == "" {
			return createErrorResponse(errors.InvalidInput("command is required for shell tasks"))
		}
	}

	s.logger.Debugf("Handling add_manual_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, "", params.Description, params.Enabled) // No schedule
	task.Type = params.Type
	task.Command = params.Command
	task.Prompt = params.Prompt
	task.RunOnDemandOnly = true
	task.TriggerType = model.TriggerTypeManual

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleTriggerDependencyChain manually triggers a dependency chain
func (s *MCPServer) handleTriggerDependencyChain(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Infof("Triggering dependency chain starting with task %s", taskID)

	// Manually trigger the task using the exported method
	s.scheduler.TriggerTask(task) // Changed from triggerTask to TriggerTask

	return createSuccessResponse(fmt.Sprintf("Dependency chain triggered starting with task %s", taskID))
}
