// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"fmt"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
)

// ToolDefinition represents a tool that can be registered with the MCP server
type ToolDefinition struct {
	// Name is the name of the tool
	Name string
	// Description is a brief description of what the tool does
	Description string
	// Handler is the function that will be called when the tool is invoked
	Handler func(*protocol.CallToolRequest) (*protocol.CallToolResult, error)
	// Parameters is the parameter schema for the tool (can be a struct)
	Parameters interface{}
}

func (s *MCPServer) registerToolsDeclarative() {
	// Define all the tools in one place
	tools := []ToolDefinition{
		{
			Name:        "list_tasks",
			Description: "Lists all scheduled tasks",
			Handler:     s.handleListTasks,
			Parameters:  struct{}{},
		},
		{
			Name:        "get_task",
			Description: "Gets a specific task by ID",
			Handler:     s.handleGetTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "add_task",
			Description: "Adds a new scheduled shell command task",
			Handler:     s.handleAddTask,
			Parameters:  TaskParams{},
		},
		{
			Name:        "add_ai_task",
			Description: "Adds a new scheduled AI (LLM) task. Use the 'prompt' field to directly specify what the AI should do.",
			Handler:     s.handleAddAITask,
			Parameters:  AITaskParams{},
		},
		{
			Name:        "update_task",
			Description: "Updates an existing task",
			Handler:     s.handleUpdateTask,
			Parameters:  AITaskParams{},
		},
		{
			Name:        "remove_task",
			Description: "Removes a task by ID",
			Handler:     s.handleRemoveTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "enable_task",
			Description: "Enables a disabled task",
			Handler:     s.handleEnableTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "disable_task",
			Description: "Disables an enabled task",
			Handler:     s.handleDisableTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "run_task",
			Description: "Triggers any configured scheduled task to run immediately on demand",
			Handler:     s.handleRunTask,
			Parameters:  RunTaskParams{},
		},
		{
			Name:        "list_run_status",
			Description: "Lists recent task execution status and summaries",
			Handler:     s.handleListRunStatus,
			Parameters:  RunStatusParams{},
		},
		{
			Name:        "get_run_output",
			Description: "Gets detailed output from a specific task run",
			Handler:     s.handleGetRunOutput,
			Parameters:  RunOutputParams{},
		},
		{
			Name:        "search_runs",
			Description: "Searches task runs with filters (status, name, date)",
			Handler:     s.handleSearchRuns,
			Parameters:  RunSearchParams{},
		},
		{
			Name:        "export_runs",
			Description: "Exports run data in various formats (JSON, CSV, Markdown)",
			Handler:     s.handleExportRuns,
			Parameters:  ExportRunsParams{},
		},
		{
			Name:        "create_agent",
			Description: "Create a new autonomous AI agent with persistent conversation and memory",
			Handler:     s.handleCreateAgent,
			Parameters:  AgentParams{},
		},
		{
			Name:        "spawn_agent",
			Description: "Spawn a new agent using natural language description",
			Handler:     s.handleSpawnAgent,
			Parameters:  SpawnAgentParams{},
		},
		{
			Name:        "add_dependency_task",
			Description: "Adds a new task that depends on other tasks completing first",
			Handler:     s.handleAddDependencyTask,
			Parameters:  DependencyTaskParams{},
		},
		{
			Name:        "add_watcher_task",
			Description: "Adds a new watcher task that triggers on file changes or task completions",
			Handler:     s.handleAddWatcherTask,
			Parameters:  WatcherTaskParams{},
		},
		{
			Name:        "add_manual_task",
			Description: "Adds a new task that only runs when manually triggered",
			Handler:     s.handleAddManualTask,
			Parameters:  AITaskParams{},
		},
		{
			Name:        "trigger_dependency_chain",
			Description: "Manually triggers a dependency chain starting from a specific task",
			Handler:     s.handleTriggerDependencyChain,
			Parameters:  TaskIDParams{},
		},
		// Add these observability tools:
		{
			Name:        "get_metrics",
			Description: "Get comprehensive metrics about task execution and system performance",
			Handler:     s.handleGetMetrics,
			Parameters:  MetricsParams{},
		},
		{
			Name:        "health_check",
			Description: "Perform health checks and return system status",
			Handler:     s.handleHealthCheck,
			Parameters:  HealthCheckParams{},
		},
		{
			Name:        "get_system_metrics",
			Description: "Get detailed system performance metrics",
			Handler:     s.handleGetSystemMetrics,
			Parameters:  struct{}{}, // No parameters needed
		},

		// Add maintenance window tools:
		{
			Name:        "add_maintenance_window",
			Description: "Add a maintenance window during which tasks will be skipped",
			Handler:     s.handleAddMaintenanceWindow,
			Parameters:  MaintenanceWindowParams{},
		},
		{
			Name:        "add_time_window",
			Description: "Add a time window constraint for task execution",
			Handler:     s.handleAddTimeWindow,
			Parameters:  TimeWindowParams{},
		},
		{
			Name:        "list_holidays",
			Description: "List holidays for a specific date range and timezone",
			Handler:     s.handleListHolidays,
			Parameters:  HolidayQueryParams{},
		},
		{
			Name:        "list_models",
			Description: "List all available models from OpenRouter and OLLAMA",
			Handler:     s.handleListModels,
			Parameters:  struct{}{},
		},
		{
			Name:        "batch_operations",
			Description: "Perform multiple task operations in a single request with optional atomic transactions",
			Handler:     s.handleBatchOperations,
			Parameters:  BatchOperationParams{},
		},
	}

	// Register all the tools
	for _, tool := range tools {
		registerToolWithError(s.server, tool)
	}
}

// registerToolWithError registers a tool with error handling
func registerToolWithError(srv *server.Server, def ToolDefinition) {
	// Convert Go struct types to JSON schema format that protocol.NewTool expects
	var jsonSchema interface{}

	// Convert the parameter struct to JSON schema
	switch p := def.Parameters.(type) {
	case struct{}:
		// Empty struct - no parameters
		jsonSchema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	case TaskIDParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "the ID of the task",
				},
			},
			"required": []string{"id"},
		}
	case TaskParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":          map[string]interface{}{"type": "string", "description": "task ID"},
				"name":        map[string]interface{}{"type": "string", "description": "task name"},
				"schedule":    map[string]interface{}{"type": "string", "description": "cron schedule expression"},
				"type":        map[string]interface{}{"type": "string", "description": "task type"},
				"command":     map[string]interface{}{"type": "string", "description": "command to execute"},
				"description": map[string]interface{}{"type": "string", "description": "task description"},
				"enabled":     map[string]interface{}{"type": "boolean", "description": "whether the task is enabled"},
			},
		}
	case AITaskParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":           map[string]interface{}{"type": "string", "description": "task ID"},
				"name":         map[string]interface{}{"type": "string", "description": "task name"},
				"schedule":     map[string]interface{}{"type": "string", "description": "cron schedule expression"},
				"type":         map[string]interface{}{"type": "string", "description": "task type"},
				"command":      map[string]interface{}{"type": "string", "description": "command to execute"},
				"description":  map[string]interface{}{"type": "string", "description": "task description"},
				"enabled":      map[string]interface{}{"type": "boolean", "description": "whether the task is enabled"},
				"prompt":       map[string]interface{}{"type": "string", "description": "prompt to use for AI"},
				"model":        map[string]interface{}{"type": "string", "description": "specific model to use"},
				"modelHint":    map[string]interface{}{"type": "string", "description": "hint for model selection"},
				"requireLocal": map[string]interface{}{"type": "boolean", "description": "require local model execution"},
				"maxCost":      map[string]interface{}{"type": "number", "description": "maximum cost per execution"},
			},
		}
	case RunTaskParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":      map[string]interface{}{"type": "string", "description": "the ID of the task to run"},
				"timeout": map[string]interface{}{"type": "string", "description": "optional timeout duration"},
			},
			"required": []string{"id"},
		}
	case AgentParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "agent name"},
				"schedule":    map[string]interface{}{"type": "string", "description": "cron schedule expression"},
				"prompt":      map[string]interface{}{"type": "string", "description": "main task/goal for the agent"},
				"personality": map[string]interface{}{"type": "string", "description": "agent personality and role"},
				"description": map[string]interface{}{"type": "string", "description": "agent description"},
				"context":     map[string]interface{}{"type": "string", "description": "additional context"},
				"enabled":     map[string]interface{}{"type": "boolean", "description": "whether the agent is enabled"},
			},
			"required": []string{"name", "schedule", "prompt", "personality"},
		}
	case SpawnAgentParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"description": map[string]interface{}{"type": "string", "description": "natural language description of the agent to create"},
			},
			"required": []string{"description"},
		}
	case DependencyTaskParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":         map[string]interface{}{"type": "string", "description": "task name"},
				"schedule":     map[string]interface{}{"type": "string", "description": "cron schedule expression"},
				"dependencies": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "list of task IDs this task depends on"},
				// Add other fields as needed
			},
			"required": []string{"name", "schedule", "dependencies"},
		}
	case WatcherTaskParams:
		jsonSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":      map[string]interface{}{"type": "string", "description": "task name"},
				"watchPath": map[string]interface{}{"type": "string", "description": "path to watch for changes"},
				"pattern":   map[string]interface{}{"type": "string", "description": "file pattern to match"},
				// Add other fields as needed
			},
			"required": []string{"name", "watchPath"},
		}
	default:
		// For any other types, try to use reflection to build schema
		jsonSchema = buildSchemaFromStruct(p)
	}

	tool, err := protocol.NewTool(def.Name, def.Description, jsonSchema)
	if err != nil {
		panic(fmt.Errorf("failed to register tool '%s': %w", def.Name, err))
	}

	srv.RegisterTool(tool, def.Handler)
}

// buildSchemaFromStruct builds a JSON schema from a struct using reflection
func buildSchemaFromStruct(v interface{}) interface{} {
	// This is a fallback for any parameter types we haven't explicitly handled
	return map[string]interface{}{
		"type":        "object",
		"properties":  map[string]interface{}{},
		"description": "Dynamic schema for " + fmt.Sprintf("%T", v),
	}
}
