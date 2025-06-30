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
		{
			Name:        "list_models",
			Description: "List all available models from OpenRouter and OLLAMA",
			Handler:     s.handleListModels,
			Parameters:  struct{}{},
		},
	}

	// Register all the tools
	for _, tool := range tools {
		registerToolWithError(s.server, tool)
	}
}

func registerToolWithError(srv *server.Server, def ToolDefinition) {
	fmt.Printf("[DEBUG] Attempting to register tool: %s\n", def.Name)
	fmt.Printf("[DEBUG] Parameter type: %T\n", def.Parameters)

	// The go-mcp library expects the actual Go struct, not a JSON schema map
	// Let's try passing the struct directly first
	tool, err := protocol.NewTool(def.Name, def.Description, def.Parameters)
	if err != nil {
		fmt.Printf("[ERROR] Direct struct failed for %s: %v\n", def.Name, err)

		// If that fails, try with nil for empty structs
		var params interface{}
		switch def.Parameters.(type) {
		case struct{}:
			params = nil // Use nil instead of empty struct
		default:
			params = def.Parameters
		}

		tool, err = protocol.NewTool(def.Name, def.Description, params)
		if err != nil {
			fmt.Printf("[ERROR] Alternative approach failed for %s: %v\n", def.Name, err)
			panic(fmt.Errorf("failed to register tool '%s': %w", def.Name, err))
		}
	}

	fmt.Printf("[DEBUG] protocol.NewTool succeeded for: %s\n", def.Name)
	srv.RegisterTool(tool, def.Handler)
	fmt.Printf("[DEBUG] Successfully registered tool: %s\n", def.Name)
}
