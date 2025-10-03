// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"mcp-cron-persistent/internal/errors"
	"mcp-cron-persistent/internal/model"
	"mcp-cron-persistent/internal/utils"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
)

type UnifiedTaskParams struct {
	Name        string `json:"name,omitempty"`
	Schedule    string `json:"schedule,omitempty"`
	Content     string `json:"content,omitempty"` // Can be command or prompt
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	Type        string `json:"type,omitempty"`
}

// ChatContext holds chat session information passed from mcp-compose
type ChatContext struct {
	SessionID  string
	OutputToChat bool
	Provider   string
	Model      string
	MCPServers []string
}

// extractParams extracts parameters from a tool request
func extractParams(request *protocol.CallToolRequest, params interface{}) error {
	if err := utils.JsonUnmarshal(request.RawArguments, params); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid parameters: %v", err))
	}
	return nil
}

// extractChatContext extracts chat context from MCP tool request arguments
func extractChatContext(request *protocol.CallToolRequest) *ChatContext {
	var rawArgs map[string]interface{}
	if err := utils.JsonUnmarshal(request.RawArguments, &rawArgs); err != nil {
		log.Printf("[INFO] Failed to unmarshal raw arguments for chat context extraction: %v", err)
		return nil
	}

	context := &ChatContext{}

	if sessionID, ok := rawArgs["_chat_session_id"].(string); ok {
		context.SessionID = sessionID
		log.Printf("[INFO] EXTRACTION: Found _chat_session_id=%s", sessionID)
	}

	if outputToChat, ok := rawArgs["_output_to_chat"].(bool); ok {
		context.OutputToChat = outputToChat
		log.Printf("[INFO] EXTRACTION: Found _output_to_chat=%v", outputToChat)
	}

	if provider, ok := rawArgs["_provider"].(string); ok {
		context.Provider = provider
		log.Printf("[INFO] EXTRACTION: Found _provider=%s", provider)
	}

	if model, ok := rawArgs["_model"].(string); ok {
		context.Model = model
		log.Printf("[INFO] EXTRACTION: Found _model=%s", model)
	}

	if mcpServers, ok := rawArgs["_mcp_servers"].([]interface{}); ok {
		context.MCPServers = make([]string, 0, len(mcpServers))
		for _, server := range mcpServers {
			if serverStr, ok := server.(string); ok {
				context.MCPServers = append(context.MCPServers, serverStr)
			}
		}
		log.Printf("[INFO] EXTRACTION: Found %d MCP servers", len(context.MCPServers))
	}

	if context.SessionID == "" && !context.OutputToChat {
		log.Printf("[INFO] EXTRACTION: No valid chat context found in request")
		return nil
	}

	log.Printf("[INFO] EXTRACTION: Successfully extracted chat context: session=%s, provider=%s, model=%s", context.SessionID, context.Provider, context.Model)
	return context
}

// extractTaskIDParam extracts the task ID parameter from a request
func extractTaskIDParam(request *protocol.CallToolRequest) (string, error) {
	var params TaskIDParams
	if err := extractParams(request, &params); err != nil {
		return "", err
	}

	if params.ID == "" {
		return "", errors.InvalidInput("task ID is required")
	}

	return params.ID, nil
}

// createSuccessResponse creates a success response
func createSuccessResponse(message string) (*protocol.CallToolResult, error) {
	response := map[string]interface{}{
		"success": true,
		"message": message,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal response: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

// createErrorResponse creates an error response
func createErrorResponse(err error) (*protocol.CallToolResult, error) {
	// Always return the original error as the second return value
	// This ensures MCP protocol error handling works correctly
	return nil, err
}

// createTaskResponse creates a response with a single task
func createTaskResponse(task *model.Task) (*protocol.CallToolResult, error) {
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal task: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(taskJSON),
			},
		},
	}, nil
}

// createTasksResponse creates a response with multiple tasks
func createTasksResponse(tasks []*model.Task) (*protocol.CallToolResult, error) {
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal tasks: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(tasksJSON),
			},
		},
	}, nil
}

// validateTaskParams validates the common task parameters
func validateTaskParams(name, schedule string) error {
	if name == "" || schedule == "" {
		return errors.InvalidInput("missing required fields: name and schedule are required")
	}
	return nil
}

// validateShellTaskParams validates the parameters specific to shell tasks
func validateShellTaskParams(name, schedule, command string) error {
	if err := validateTaskParams(name, schedule); err != nil {
		return err
	}

	if command == "" {
		return errors.InvalidInput("missing required field: command is required for shell tasks")
	}

	return nil
}

// validateAITaskParams validates the parameters specific to AI tasks
func validateAITaskParams(name, schedule, prompt string) error {
	if err := validateTaskParams(name, schedule); err != nil {
		return err
	}

	if prompt == "" {
		return errors.InvalidInput("missing required field: prompt is required for AI tasks")
	}

	return nil
}

// BatchOperationParams for bulk operations
type BatchOperationParams struct {
	Operations []BatchOperation `json:"operations" description:"Array of operations to perform"`
	Atomic     bool             `json:"atomic,omitempty" description:"All operations succeed or all fail"`
	DryRun     bool             `json:"dryRun,omitempty" description:"Preview operations without executing"`
}

type BatchOperation struct {
	Action     string      `json:"action" description:"Operation type: create, update, delete, enable, disable"`
	TaskData   interface{} `json:"taskData,omitempty" description:"Task data for create/update operations"`
	TaskID     string      `json:"taskId,omitempty" description:"Task ID for update/delete/enable/disable operations"`
	Parameters interface{} `json:"parameters,omitempty" description:"Additional parameters for the operation"`
}

type BatchResult struct {
	Success bool              `json:"success"`
	Results []OperationResult `json:"results"`
	Summary BatchSummary      `json:"summary"`
	Errors  []string          `json:"errors,omitempty"`
}

type OperationResult struct {
	Index   int         `json:"index"`
	Action  string      `json:"action"`
	Success bool        `json:"success"`
	TaskID  string      `json:"taskId,omitempty"`
	Error   string      `json:"error,omitempty"`
	Task    *model.Task `json:"task,omitempty"`
}

type BatchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

func (s *MCPServer) handleBatchOperations(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params BatchOperationParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if len(params.Operations) == 0 {
		return createErrorResponse(errors.InvalidInput("no operations specified"))
	}

	s.logger.Infof("Executing batch operation with %d operations (atomic: %v, dryRun: %v)",
		len(params.Operations), params.Atomic, params.DryRun)

	result := BatchResult{
		Results: make([]OperationResult, len(params.Operations)),
		Summary: BatchSummary{Total: len(params.Operations)},
	}

	var allErrors []string

	// Execute operations
	for i, op := range params.Operations {
		opResult := OperationResult{
			Index:  i,
			Action: op.Action,
		}

		if params.DryRun {
			// Validate operation without executing
			if err := s.validateBatchOperation(op); err != nil {
				opResult.Error = err.Error()
				opResult.Success = false
			} else {
				opResult.Success = true
				opResult.TaskID = fmt.Sprintf("dry-run-%d", i)
			}
		} else {
			// Execute operation
			err := s.executeBatchOperation(op, &opResult)
			if err != nil {
				opResult.Success = false
				opResult.Error = err.Error()
				allErrors = append(allErrors, fmt.Sprintf("Operation %d (%s): %v", i, op.Action, err))

				if params.Atomic {
					// Rollback previous operations
					s.rollbackOperations(result.Results[:i])
					result.Success = false
					result.Errors = allErrors
					break
				}
			} else {
				opResult.Success = true
			}
		}

		result.Results[i] = opResult
		if opResult.Success {
			result.Summary.Succeeded++
		} else {
			result.Summary.Failed++
		}
	}

	if !params.Atomic || len(allErrors) == 0 {
		result.Success = true
	}

	if len(allErrors) > 0 {
		result.Errors = allErrors
	}

	responseJSON, err := json.Marshal(result)
	if err != nil {
		return createErrorResponse(errors.Internal(err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

func (s *MCPServer) validateBatchOperation(op BatchOperation) error {
	switch op.Action {
	case "create":
		if op.TaskData == nil {
			return errors.InvalidInput("taskData is required for create operation")
		}
	case "update", "delete", "enable", "disable":
		if op.TaskID == "" {
			return errors.InvalidInput("taskId is required for " + op.Action + " operation")
		}
	default:
		return errors.InvalidInput("unknown operation: " + op.Action)
	}
	return nil
}

func (s *MCPServer) executeBatchOperation(op BatchOperation, result *OperationResult) error {
	switch op.Action {
	case "create":
		return s.executeBatchCreate(op, result)
	case "update":
		return s.executeBatchUpdate(op, result)
	case "delete":
		return s.executeBatchDelete(op, result)
	case "enable":
		return s.executeBatchEnable(op, result)
	case "disable":
		return s.executeBatchDisable(op, result)
	default:
		return fmt.Errorf("unknown operation: %s", op.Action)
	}
}

func (s *MCPServer) executeBatchCreate(op BatchOperation, result *OperationResult) error {
	// Convert taskData to UnifiedTaskParams
	taskJSON, err := json.Marshal(op.TaskData)
	if err != nil {
		return err
	}

	var params UnifiedTaskParams
	if err := json.Unmarshal(taskJSON, &params); err != nil {
		return err
	}

	// Create task using unified handler logic
	taskType := s.detectTaskType(params.Content)
	if params.Name == "" {
		params.Name = s.generateTaskName(params.Content)
	}

	task := createBaseTask(params.Name, params.Schedule, "", params.Enabled)
	task.Type = taskType

	if taskType == model.TypeAI.String() {
		task.Prompt = params.Content
	} else {
		task.Command = params.Content
	}

	if err := s.scheduler.AddTask(task); err != nil {
		return err
	}

	result.TaskID = task.ID
	result.Task = task
	return nil
}

func (s *MCPServer) executeBatchUpdate(op BatchOperation, result *OperationResult) error {
	if op.TaskID == "" {
		return errors.InvalidInput("taskId is required for update operation")
	}

	task, err := s.scheduler.GetTask(op.TaskID)
	if err != nil {
		return err
	}

	if op.TaskData != nil {
		// Convert taskData to AITaskParams for compatibility
		taskJSON, err := json.Marshal(op.TaskData)
		if err != nil {
			return err
		}

		var params AITaskParams
		if err := json.Unmarshal(taskJSON, &params); err != nil {
			return err
		}

		// Update task fields
		updateTaskFields(task, params, taskJSON)
	}

	if err := s.scheduler.UpdateTask(task); err != nil {
		return err
	}

	result.TaskID = task.ID
	result.Task = task
	return nil
}

func (s *MCPServer) executeBatchDelete(op BatchOperation, result *OperationResult) error {
	if err := s.scheduler.RemoveTask(op.TaskID); err != nil {
		return err
	}
	result.TaskID = op.TaskID
	return nil
}

func (s *MCPServer) executeBatchEnable(op BatchOperation, result *OperationResult) error {
	if err := s.scheduler.EnableTask(op.TaskID); err != nil {
		return err
	}

	task, err := s.scheduler.GetTask(op.TaskID)
	if err != nil {
		return err
	}

	result.TaskID = op.TaskID
	result.Task = task
	return nil
}

func (s *MCPServer) executeBatchDisable(op BatchOperation, result *OperationResult) error {
	if err := s.scheduler.DisableTask(op.TaskID); err != nil {
		return err
	}

	task, err := s.scheduler.GetTask(op.TaskID)
	if err != nil {
		return err
	}

	result.TaskID = op.TaskID
	result.Task = task
	return nil
}

func (s *MCPServer) rollbackOperations(operations []OperationResult) {
	for i := len(operations) - 1; i >= 0; i-- {
		op := operations[i]
		if !op.Success {
			continue
		}

		switch op.Action {
		case "create":
			if op.TaskID != "" {
				s.scheduler.RemoveTask(op.TaskID)
				s.logger.Infof("Rolled back create operation for task %s", op.TaskID)
			}
		case "delete":
			s.logger.Warnf("Cannot rollback delete operation for task %s", op.TaskID)
		case "enable":
			if op.TaskID != "" {
				s.scheduler.DisableTask(op.TaskID)
				s.logger.Infof("Rolled back enable operation for task %s", op.TaskID)
			}
		case "disable":
			if op.TaskID != "" {
				s.scheduler.EnableTask(op.TaskID)
				s.logger.Infof("Rolled back disable operation for task %s", op.TaskID)
			}
		}
	}
}

func (s *MCPServer) handleListModels(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	s.logger.Debugf("Handling list_models request")

	var models []map[string]interface{}

	// Get OpenRouter models if enabled
	if s.config.UseOpenRouter || s.config.OpenRouter.Enabled {
		openRouterModels := []map[string]interface{}{
			{
				"name":       "anthropic/claude-3.5-sonnet",
				"provider":   "anthropic",
				"type":       "remote",
				"cost":       "high",
				"speed":      "medium",
				"capability": "very_high",
			},
			{
				"name":       "openai/gpt-4o-mini",
				"provider":   "openai",
				"type":       "remote",
				"cost":       "low",
				"speed":      "very_high",
				"capability": "high",
			},
		}
		models = append(models, openRouterModels...)
	}

	// Get OLLAMA models if enabled
	if s.config.Ollama.Enabled {
		_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client := &http.Client{Timeout: 10 * time.Second}
		url := s.config.Ollama.BaseURL + "/api/tags"

		resp, err := client.Get(url)
		if err != nil {
			s.logger.Warnf("Failed to connect to OLLAMA: %v", err)
		} else {
			defer resp.Body.Close()

			var ollamaResp struct {
				Models []struct {
					Name       string    `json:"name"`
					Size       int64     `json:"size"`
					ModifiedAt time.Time `json:"modified_at"`
				} `json:"models"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
				s.logger.Warnf("Failed to parse OLLAMA response: %v", err)
			} else {
				for _, model := range ollamaResp.Models {
					models = append(models, map[string]interface{}{
						"name":       "local/" + model.Name,
						"provider":   "ollama",
						"type":       "local",
						"cost":       "free",
						"speed":      "variable",
						"capability": "variable",
						"size":       model.Size,
						"modified":   model.ModifiedAt,
					})
				}
			}
		}
	}

	response := map[string]interface{}{
		"models":               models,
		"total":                len(models),
		"model_router_enabled": s.config.ModelRouter.Enabled,
		"ollama_enabled":       s.config.Ollama.Enabled,
		"openrouter_enabled":   s.config.UseOpenRouter || s.config.OpenRouter.Enabled,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return createErrorResponse(errors.Internal(err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}
