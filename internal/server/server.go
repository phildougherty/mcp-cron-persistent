// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp-cron-persistent/internal/activity"
	"mcp-cron-persistent/internal/agent"
	"mcp-cron-persistent/internal/command"
	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/errors"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
	"mcp-cron-persistent/internal/observability"
	"mcp-cron-persistent/internal/scheduler"
	"mcp-cron-persistent/internal/utils"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

// Make os.OpenFile mockable for testing
var osOpenFile = os.OpenFile

// TaskParams holds parameters for various task operations
type TaskParams struct {
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
}

// TaskIDParams holds the ID parameter used by multiple handlers
type TaskIDParams struct {
	ID string `json:"id" description:"the ID of the task to get/remove/enable/disable"`
}

// AITaskParams combines task parameters with AI parameters
type AITaskParams struct {
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
	// LLM Prompt
	Prompt       string  `json:"prompt,omitempty" description:"prompt to use for AI"`
	Model        string  `json:"model,omitempty" description:"specific model to use"`
	ModelHint    string  `json:"modelHint,omitempty" description:"hint for model selection: fast, cheap, powerful, local, balanced"`
	RequireLocal bool    `json:"requireLocal,omitempty" description:"require local model execution"`
	MaxCost      float64 `json:"maxCost,omitempty" description:"maximum cost per execution"`
}

// AgentParams holds parameters for agent creation
type AgentParams struct {
	Name        string `json:"name" description:"agent name"`
	Schedule    string `json:"schedule" description:"cron schedule expression"`
	Prompt      string `json:"prompt" description:"main task/goal for the agent"`
	Personality string `json:"personality" description:"agent personality and role"`
	Description string `json:"description,omitempty" description:"agent description"`
	Context     string `json:"context,omitempty" description:"additional context"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the agent is enabled"`
}

// SpawnAgentParams holds parameters for natural language agent spawning
type SpawnAgentParams struct {
	Description string `json:"description" description:"natural language description of the agent to create"`
}

// AgentSpecification represents a parsed agent specification
type AgentSpecification struct {
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	Prompt      string `json:"prompt"`
	Personality string `json:"personality"`
	Description string `json:"description"`
	Context     string `json:"context"`
}

// RunTaskParams holds parameters for running a task on demand
type RunTaskParams struct {
	ID      string `json:"id" description:"the ID of the task to run"`
	Timeout string `json:"timeout,omitempty" description:"optional timeout duration (e.g., '5m', '30s'). Uses task's default if not specified"`
}

// MCPServer represents the MCP scheduler server
type MCPServer struct {
	scheduler        *scheduler.Scheduler
	cmdExecutor      *command.CommandExecutor
	agentExecutor    *agent.AgentExecutor
	server           *server.Server
	httpServer       *http.Server
	address          string
	port             int
	stopCh           chan struct{}
	wg               sync.WaitGroup
	config           *config.Config
	logger           *logging.Logger
	shutdownMutex    sync.Mutex
	isShuttingDown   bool
	metricsCollector *observability.MetricsCollector
	storage          scheduler.Storage
}

// NewMCPServer creates a new MCP scheduler server
func NewMCPServer(cfg *config.Config, scheduler *scheduler.Scheduler, cmdExecutor *command.CommandExecutor, agentExecutor *agent.AgentExecutor) (*MCPServer, error) {
	// Create default config if not provided
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Initialize logger
	var logger *logging.Logger
	if cfg.Logging.FilePath != "" {
		var err error
		logger, err = logging.FileLogger(cfg.Logging.FilePath, parseLogLevel(cfg.Logging.Level))
		if err != nil {
			return nil, fmt.Errorf("failed to create file logger: %w", err)
		}
	} else {
		logger = logging.New(logging.Options{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	}

	// Set as the default logger
	logging.SetDefaultLogger(logger)

	// Configure logger based on transport mode
	if cfg.Server.TransportMode == "stdio" {
		// For stdio transport, we need to be careful with logging
		// as it could interfere with JSON-RPC messages
		// Redirect logs to a file instead of stdout
		// Get the executable path
		execPath, err := os.Executable()
		if err != nil {
			logger.Errorf("Failed to get executable path: %v", err)
			execPath = cfg.Server.Name
		}
		// Get the directory containing the executable
		execDir := filepath.Dir(execPath)
		// Set log path in the same directory as the executable
		logFilename := fmt.Sprintf("%s.log", cfg.Server.Name)
		logPath := filepath.Join(execDir, logFilename)
		logFile, err := osOpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			log.SetOutput(logFile)
			logger.Infof("Logging to %s", logPath)
		} else {
			logger.Errorf("Failed to open log file at %s: %v", logPath, err)
		}
	}

	// Create metrics collector
	metricsCollector := observability.NewMetricsCollector(logger)

	// Create MCP Server
	mcpServer := &MCPServer{
		scheduler:        scheduler,
		cmdExecutor:      cmdExecutor,
		agentExecutor:    agentExecutor,
		address:          cfg.Server.Address,
		port:             cfg.Server.Port,
		stopCh:           make(chan struct{}),
		config:           cfg,
		logger:           logger,
		metricsCollector: metricsCollector, // Add this
		// storage will be set when scheduler.SetStorage is called
	}

	// Set up scheduler with metrics collector
	scheduler.SetMetricsCollector(metricsCollector)

	// NOTE: Do NOT set task executor here - it will be set in main.go
	// This prevents the circular dependency issue

	// Create transport based on mode
	var svrTransport transport.ServerTransport
	var err error
	switch cfg.Server.TransportMode {
	case "stdio":
		// Create stdio transport
		logger.Infof("Using stdio transport")
		svrTransport = transport.NewStdioServerTransport()
	case "sse":
		// Create HTTP SSE transport
		addr := fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port)
		logger.Infof("Using SSE transport on %s", addr)
		// Create SSE transport with the address
		svrTransport, err = transport.NewSSEServerTransport(addr)
		if err != nil {
			return nil, errors.Internal(fmt.Errorf("failed to create SSE transport: %w", err))
		}
	default:
		return nil, errors.InvalidInput(fmt.Sprintf("unsupported transport mode: %s", cfg.Server.TransportMode))
	}

	// Create MCP server with the transport
	mcpServer.server, err = server.NewServer(
		svrTransport,
		server.WithServerInfo(protocol.Implementation{
			Name:    cfg.Server.Name,
			Version: cfg.Server.Version,
		}),
	)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to create MCP server: %w", err))
	}

	// Set up health check server for SSE transport mode
	logger.Infof("DEBUG: Transport mode is %s", cfg.Server.TransportMode)
	if cfg.Server.TransportMode == "sse" {
		healthPort := cfg.Server.Port + 1000
		healthAddr := fmt.Sprintf("%s:%d", cfg.Server.Address, healthPort)
		logger.Infof("Setting up health check endpoints on %s", healthAddr)
		
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			mcpServer.handleHTTPHealthCheck(w, r)
		})
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			mcpServer.handleHTTPReadinessCheck(w, r)
		})
		
		mcpServer.httpServer = &http.Server{
			Addr:    healthAddr,
			Handler: mux,
		}
	}

	return mcpServer, nil
}

// Start starts the MCP server
func (s *MCPServer) Start(ctx context.Context) error {
	// Register all tools
	s.registerToolsDeclarative()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Start the server
		if err := s.server.Run(); err != nil {
			s.logger.Errorf("Error running MCP server: %v", err)
			return
		}
	}()

	// Start health check server if available
	if s.httpServer != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.logger.Infof("Starting health check server on %s", s.httpServer.Addr)
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Errorf("Error running health check server: %v", err)
			}
		}()
	}

	// Listen for context cancellation
	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			s.logger.Errorf("Error stopping MCP server: %v", err)
		}
	}()

	return nil
}

// SetStorage sets the storage reference (called when scheduler storage is set)
func (s *MCPServer) SetStorage(storage scheduler.Storage) {
	s.storage = storage
}

// handleRunTask triggers a task to run immediately
func (s *MCPServer) handleRunTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params RunTaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.ID == "" {
		return createErrorResponse(errors.InvalidInput("task ID is required"))
	}

	s.logger.Debugf("Handling run_task request for task %s", params.ID)

	// Get the task
	task, err := s.scheduler.GetTask(params.ID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Broadcast task start
	activity.BroadcastActivity("INFO", "task",
		fmt.Sprintf("Task '%s' started (manual trigger)", task.Name),
		map[string]interface{}{
			"taskId":    task.ID,
			"taskName":  task.Name,
			"taskType":  task.Type,
			"trigger":   "manual",
			"startTime": time.Now(),
		})

	// Parse timeout if provided
	timeout := s.config.Scheduler.DefaultTimeout
	if params.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(params.Timeout); err != nil {
			return createErrorResponse(errors.InvalidInput(fmt.Sprintf("invalid timeout format: %s", err.Error())))
		} else {
			timeout = parsedTimeout
		}
	}

	// Create context for execution
	ctx := context.Background()

	// Update task status to running
	task.Status = model.StatusRunning
	task.LastRun = time.Now()
	task.UpdatedAt = time.Now()

	// Execute the task using the server's Execute method (which routes to appropriate executor)
	s.logger.Infof("Executing task %s (%s) on demand", task.ID, task.Name)
	startTime := time.Now()
	err = s.Execute(ctx, task, timeout)
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// Update task status based on execution outcome
	if err != nil {
		task.Status = model.StatusFailed
		s.logger.Errorf("Task %s failed: %v", task.ID, err)
		// Broadcast task failure
		activity.BroadcastActivity("ERROR", "task",
			fmt.Sprintf("Task '%s' failed: %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"error":    err.Error(),
				"duration": duration.Seconds(),
				"trigger":  "manual",
			})
	} else {
		task.Status = model.StatusCompleted
		s.logger.Infof("Task %s completed successfully", task.ID)
		// Broadcast task success
		activity.BroadcastActivity("INFO", "task",
			fmt.Sprintf("Task '%s' completed successfully in %v", task.Name, duration),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"duration": duration.Seconds(),
				"status":   "completed",
				"trigger":  "manual",
			})
	}

	// Update the task in scheduler (this will save to storage if available)
	if updateErr := s.scheduler.UpdateTask(task); updateErr != nil {
		s.logger.Errorf("Failed to update task %s after execution: %v", task.ID, updateErr)
	}

	// Create response with execution details
	response := map[string]interface{}{
		"success":    err == nil,
		"task_id":    task.ID,
		"task_name":  task.Name,
		"status":     string(task.Status),
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"duration":   duration.String(),
		"exit_code":  0, // Will be updated below if we have executor result
	}

	if err != nil {
		response["error"] = err.Error()
		response["exit_code"] = 1
	} else {
		response["message"] = fmt.Sprintf("Task %s executed successfully", task.ID)
	}

	// Get the actual result from the appropriate executor for more detailed output
	if executorResult, exists := s.GetTaskResult(task.ID); exists {
		response["output"] = executorResult.Output
		response["exit_code"] = executorResult.ExitCode

		// Add command or prompt info based on task type
		if executorResult.Command != "" {
			response["command"] = executorResult.Command
		}
		if executorResult.Prompt != "" {
			response["prompt"] = executorResult.Prompt
		}
		if executorResult.ConversationID != "" {
			response["conversation_id"] = executorResult.ConversationID
		}
	}

	responseJSON, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal response: %w", marshalErr))
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

// Stop stops the MCP server
func (s *MCPServer) Stop() error {
	s.shutdownMutex.Lock()
	defer s.shutdownMutex.Unlock()

	// Return early if server is already being shut down
	if s.isShuttingDown {
		s.logger.Debugf("Stop called but server is already shutting down, ignoring")
		return nil
	}

	s.isShuttingDown = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop the MCP server
	if err := s.server.Shutdown(ctx); err != nil {
		return errors.Internal(fmt.Errorf("error shutting down MCP server: %w", err))
	}

	// Stop the health check server
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.Errorf("Error shutting down health check server: %v", err)
		}
	}

	// Only close stopCh if it hasn't been closed yet
	select {
	case <-s.stopCh:
		// Channel is already closed, do nothing
	default:
		close(s.stopCh)
	}

	s.wg.Wait()
	return nil
}

// handleListTasks lists all tasks
func (s *MCPServer) handleListTasks(_ *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	s.logger.Debugf("Handling list_tasks request")
	// Get all tasks
	tasks := s.scheduler.ListTasks()
	return createTasksResponse(tasks)
}

// handleGetTask gets a specific task by ID
func (s *MCPServer) handleGetTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling get_task request for task %s", taskID)

	// Get the task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleAddTask adds a new shell command task
func (s *MCPServer) handleAddTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params TaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters
	if err := validateShellTaskParams(params.Name, params.Schedule, params.Command); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeShellCommand.String()
	task.Command = params.Command

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to create task '%s': %s", params.Name, err.Error()),
			map[string]interface{}{
				"taskName": params.Name,
				"taskType": "shell",
				"error":    err.Error(),
			})
		return createErrorResponse(err)
	}

	// Broadcast task creation
	activity.BroadcastActivity("INFO", "task_management",
		fmt.Sprintf("New task created: '%s' (shell command)", task.Name),
		map[string]interface{}{
			"taskId":   task.ID,
			"taskName": task.Name,
			"taskType": "shell",
			"schedule": task.Schedule,
			"enabled":  task.Enabled,
			"command":  task.Command,
		})

	return createTaskResponse(task)
}

func (s *MCPServer) handleAddAITask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters
	if err := validateAITaskParams(params.Name, params.Schedule, params.Prompt); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_ai_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeAI.String()
	task.Prompt = params.Prompt

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to create AI task '%s': %s", params.Name, err.Error()),
			map[string]interface{}{
				"taskName": params.Name,
				"taskType": "ai",
				"error":    err.Error(),
			})
		return createErrorResponse(err)
	}

	// Broadcast AI task creation
	activity.BroadcastActivity("INFO", "task_management",
		fmt.Sprintf("New AI task created: '%s'", task.Name),
		map[string]interface{}{
			"taskId":   task.ID,
			"taskName": task.Name,
			"taskType": "ai",
			"schedule": task.Schedule,
			"enabled":  task.Enabled,
			"prompt":   task.Prompt,
		})

	return createTaskResponse(task)
}

// handleCreateAgent creates a new autonomous agent
func (s *MCPServer) handleCreateAgent(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params AgentParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters
	if err := validateAgentParams(params.Name, params.Schedule, params.Prompt, params.Personality); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling create_agent request for agent %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeAI.String()
	task.Prompt = params.Prompt
	task.IsAgent = true
	task.AgentPersonality = params.Personality
	task.ConversationContext = params.Context

	// Agent tasks will get their conversation ID when first executed
	if params.Name != "" {
		task.ConversationName = fmt.Sprintf("Agent: %s", params.Name)
	}

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleSpawnAgent spawns a new agent using natural language
func (s *MCPServer) handleSpawnAgent(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params SpawnAgentParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.Description == "" {
		return createErrorResponse(errors.InvalidInput("description is required for spawning agents"))
	}

	s.logger.Debugf("Handling spawn_agent request: %s", params.Description)

	// Use an LLM to parse the natural language description into agent parameters
	agentSpec, err := s.parseAgentDescription(params.Description)
	if err != nil {
		return createErrorResponse(fmt.Errorf("failed to parse agent description: %w", err))
	}

	// Create the agent using the parsed specification
	createParams := AgentParams{
		Name:        agentSpec.Name,
		Schedule:    agentSpec.Schedule,
		Prompt:      agentSpec.Prompt,
		Personality: agentSpec.Personality,
		Description: agentSpec.Description,
		Context:     agentSpec.Context,
		Enabled:     true,
	}

	// Convert to protocol request
	createJSON, _ := json.Marshal(createParams)
	createRequest := &protocol.CallToolRequest{
		RawArguments: createJSON,
	}

	return s.handleCreateAgent(createRequest)
}

// parseAgentDescription uses an LLM to parse natural language into agent parameters
func (s *MCPServer) parseAgentDescription(description string) (*AgentSpecification, error) {
	// For now, provide a simple fallback implementation
	// In production, you would use the OpenWebUI client to parse this

	// Basic parsing for common patterns
	agentSpec := &AgentSpecification{
		Name:        "Generated Agent",
		Schedule:    "0 9 * * *", // Default to daily at 9 AM
		Prompt:      description,
		Personality: "You are a helpful AI assistant",
		Description: description,
		Context:     "",
	}

	// Try to extract schedule patterns
	if strings.Contains(strings.ToLower(description), "daily") {
		agentSpec.Schedule = "0 9 * * *"
	} else if strings.Contains(strings.ToLower(description), "hourly") {
		agentSpec.Schedule = "0 * * * *"
	} else if strings.Contains(strings.ToLower(description), "weekly") {
		agentSpec.Schedule = "0 9 * * 1"
	}

	// Extract name if possible
	if strings.Contains(strings.ToLower(description), "news") {
		agentSpec.Name = "News Agent"
		agentSpec.Personality = "You are a knowledgeable news analyst who provides concise, objective summaries"
	} else if strings.Contains(strings.ToLower(description), "health") {
		agentSpec.Name = "Health Agent"
		agentSpec.Personality = "You are a caring health advisor who provides gentle, informed guidance"
	} else if strings.Contains(strings.ToLower(description), "weather") {
		agentSpec.Name = "Weather Agent"
		agentSpec.Personality = "You are a friendly weather forecaster who provides clear, actionable weather information"
	}

	return agentSpec, nil
}

// createBaseTask creates a base task with common fields initialized
func createBaseTask(name, schedule, description string, enabled bool) *model.Task {
	now := time.Now()
	taskID := fmt.Sprintf("task_%d", now.UnixNano())

	return &model.Task{
		ID:          taskID,
		Name:        name,
		Schedule:    schedule,
		Description: description,
		Enabled:     enabled,
		Status:      model.StatusPending,
		LastRun:     now,
		NextRun:     now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// handleUpdateTask updates an existing task
func (s *MCPServer) handleUpdateTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.ID == "" {
		return createErrorResponse(errors.InvalidInput("task ID is required"))
	}

	s.logger.Debugf("Handling update_task request for task %s", params.ID)

	// Get existing task
	existingTask, err := s.scheduler.GetTask(params.ID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Update fields with provided values
	updateTaskFields(existingTask, params, request.RawArguments)

	// Update task in scheduler
	if err := s.scheduler.UpdateTask(existingTask); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(existingTask)
}

// updateTaskFields updates task fields with provided values
func updateTaskFields(task *model.Task, params AITaskParams, rawJSON []byte) {
	// Update non-empty string fields
	if params.Name != "" {
		task.Name = params.Name
	}
	if params.Schedule != "" {
		task.Schedule = params.Schedule
	}
	if params.Command != "" {
		task.Command = params.Command
	}
	if params.Prompt != "" {
		task.Prompt = params.Prompt
	}
	if params.Description != "" {
		task.Description = params.Description
	}

	// Update task type if provided
	if params.Type != "" {
		if strings.EqualFold(params.Type, model.TypeAI.String()) {
			task.Type = model.TypeAI.String()
		} else if strings.EqualFold(params.Type, model.TypeShellCommand.String()) {
			task.Type = model.TypeShellCommand.String()
		}
	}

	// Only update Enabled if it's explicitly in the JSON
	var rawParams map[string]interface{}
	if err := utils.JsonUnmarshal(rawJSON, &rawParams); err == nil {
		if _, exists := rawParams["enabled"]; exists {
			task.Enabled = params.Enabled
		}
	}

	task.UpdatedAt = time.Now()
}

// handleRemoveTask removes a task
func (s *MCPServer) handleRemoveTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling remove_task request for task %s", taskID)

	// Get task details before removal for broadcasting
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Store task details for broadcasting after removal
	taskName := task.Name
	taskType := task.Type

	// Remove task
	if err := s.scheduler.RemoveTask(taskID); err != nil {
		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to delete task '%s': %s", taskName, err.Error()),
			map[string]interface{}{
				"taskId":   taskID,
				"taskName": taskName,
				"error":    err.Error(),
			})
		return createErrorResponse(err)
	}

	// Broadcast task deletion
	activity.BroadcastActivity("WARN", "task_management",
		fmt.Sprintf("Task deleted: '%s'", taskName),
		map[string]interface{}{
			"taskId":   taskID,
			"taskName": taskName,
			"taskType": taskType,
		})

	return createSuccessResponse(fmt.Sprintf("Task %s removed successfully", taskID))
}

// handleEnableTask enables a task
func (s *MCPServer) handleEnableTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling enable_task request for task %s", taskID)

	// Get task details before enabling
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Enable task
	if err := s.scheduler.EnableTask(taskID); err != nil {
		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to enable task '%s': %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   taskID,
				"taskName": task.Name,
				"error":    err.Error(),
			})
		return createErrorResponse(err)
	}

	// Broadcast task enable
	activity.BroadcastActivity("INFO", "task_management",
		fmt.Sprintf("Task enabled: '%s'", task.Name),
		map[string]interface{}{
			"taskId":   taskID,
			"taskName": task.Name,
			"action":   "enabled",
		})

	// Get updated task
	updatedTask, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(updatedTask)
}

// handleDisableTask disables a task
func (s *MCPServer) handleDisableTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling disable_task request for task %s", taskID)

	// Get task details before disabling
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Disable task
	if err := s.scheduler.DisableTask(taskID); err != nil {
		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to disable task '%s': %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   taskID,
				"taskName": task.Name,
				"error":    err.Error(),
			})
		return createErrorResponse(err)
	}

	// Broadcast task disable
	activity.BroadcastActivity("INFO", "task_management",
		fmt.Sprintf("Task disabled: '%s'", task.Name),
		map[string]interface{}{
			"taskId":   taskID,
			"taskName": task.Name,
			"action":   "disabled",
		})

	// Get updated task
	updatedTask, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(updatedTask)
}

func (s *MCPServer) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	taskType := task.Type

	s.logger.Debugf("Executing task with type: %s", taskType)

	switch taskType {
	case model.TypeAI.String():
		// Use model routing if enabled
		if s.config.ModelRouter.Enabled {
			result, err := agent.RunTaskWithModelRouting(ctx, task, s.config)
			if err != nil {
				return err
			}
			// Store the result
			agentResult := &model.Result{
				TaskID:    task.ID,
				Prompt:    task.Prompt,
				Output:    result,
				StartTime: time.Now(),
				EndTime:   time.Now(),
				ExitCode:  0,
			}
			s.agentExecutor.StoreResult(task.ID, agentResult)
			return nil
		}
		// Fallback to original agent executor
		s.logger.Infof("Routing to AgentExecutor for AI task")
		return s.agentExecutor.Execute(ctx, task, timeout)
	case model.TypeShellCommand.String(), "":
		s.logger.Infof("Routing to CommandExecutor for shell command task")
		return s.cmdExecutor.Execute(ctx, task, timeout)
	default:
		return fmt.Errorf("unknown task type: %s", taskType)
	}
}

// GetTaskResult retrieves execution result for a task regardless of executor type
func (s *MCPServer) GetTaskResult(taskID string) (*model.Result, bool) {
	// First try to get the result from the agent executor
	if result, exists := s.agentExecutor.GetTaskResult(taskID); exists {
		return result, true
	}
	// If not found in agent executor, try the command executor
	return s.cmdExecutor.GetTaskResult(taskID)
}

// validateAgentParams validates the parameters for agent creation
func validateAgentParams(name, schedule, prompt, personality string) error {
	if name == "" || schedule == "" || prompt == "" || personality == "" {
		return errors.InvalidInput("missing required fields: name, schedule, prompt, and personality are required for agents")
	}
	return nil
}

// Helper function to parse log level
func parseLogLevel(level string) logging.LogLevel {
	switch level {
	case "debug":
		return logging.Debug
	case "info":
		return logging.Info
	case "warn":
		return logging.Warn
	case "error":
		return logging.Error
	case "fatal":
		return logging.Fatal
	default:
		return logging.Info
	}
}

// Add these handlers to a new file or existing server files:

func (s *MCPServer) handleAddMaintenanceWindow(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params MaintenanceWindowParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Parse times
	start, err := time.Parse(time.RFC3339, params.Start)
	if err != nil {
		return createErrorResponse(errors.InvalidInput(fmt.Sprintf("invalid start time: %s", err.Error())))
	}

	end, err := time.Parse(time.RFC3339, params.End)
	if err != nil {
		return createErrorResponse(errors.InvalidInput(fmt.Sprintf("invalid end time: %s", err.Error())))
	}

	window := &model.MaintenanceWindow{
		Name:        params.Name,
		Start:       start,
		End:         end,
		Timezone:    params.Timezone,
		Description: params.Description,
		Enabled:     params.Enabled,
	}

	if err := s.scheduler.AddMaintenanceWindow(window); err != nil {
		return createErrorResponse(err)
	}

	return createSuccessResponse(fmt.Sprintf("Maintenance window '%s' added successfully", params.Name))
}

func (s *MCPServer) handleAddTimeWindow(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params TimeWindowParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	window := &model.TimeWindow{
		Start:    params.Start,
		End:      params.End,
		Timezone: params.Timezone,
		Days:     params.Days,
	}

	if err := s.scheduler.AddTimeWindow(params.ID, window); err != nil {
		return createErrorResponse(err)
	}

	return createSuccessResponse(fmt.Sprintf("Time window '%s' added successfully", params.ID))
}

func (s *MCPServer) handleListHolidays(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params HolidayQueryParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// This is a placeholder - you'd implement actual holiday lookup
	response := map[string]interface{}{
		"holidays": []map[string]string{
			{
				"name": "New Year's Day",
				"date": params.StartDate[:4] + "-01-01",
				"type": "national",
			},
			{
				"name": "Christmas Day",
				"date": params.StartDate[:4] + "-12-25",
				"type": "national",
			},
		},
		"timezone": params.Timezone,
		"period": map[string]string{
			"start": params.StartDate,
			"end":   params.EndDate,
		},
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

func (s *MCPServer) detectTaskType(content string) string {
	content = strings.ToLower(content)

	// Look for AI/LLM keywords
	aiKeywords := []string{
		"analyze", "summarize", "generate", "explain", "describe",
		"write", "create", "suggest", "recommend", "translate",
		"what", "how", "why", "tell me", "help me",
	}

	for _, keyword := range aiKeywords {
		if strings.Contains(content, keyword) {
			return model.TypeAI.String()
		}
	}

	// Look for shell command patterns
	if strings.HasPrefix(content, "/") ||
		strings.Contains(content, "&&") ||
		strings.Contains(content, "||") ||
		strings.Contains(content, "|") ||
		strings.Contains(content, "echo") ||
		strings.Contains(content, "curl") ||
		strings.Contains(content, "wget") ||
		strings.Contains(content, "grep") ||
		strings.Contains(content, "awk") ||
		strings.Contains(content, "sed") {
		return model.TypeShellCommand.String()
	}

	// Default to AI if unsure
	return model.TypeAI.String()
}

func (s *MCPServer) generateTaskName(content string) string {
	// Clean and truncate content for name
	name := strings.TrimSpace(content)

	// Remove common prefixes
	name = strings.TrimPrefix(name, "Please ")
	name = strings.TrimPrefix(name, "Can you ")
	name = strings.TrimPrefix(name, "Could you ")

	// Truncate if too long
	if len(name) > 50 {
		name = name[:47] + "..."
	}

	// If still empty, provide default
	if name == "" {
		name = fmt.Sprintf("Task %d", time.Now().Unix())
	}

	return name
}

// handleHTTPHealthCheck handles liveness probe requests
func (s *MCPServer) handleHTTPHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Check if the server is running and not shutting down
	s.shutdownMutex.Lock()
	isShuttingDown := s.isShuttingDown
	s.shutdownMutex.Unlock()

	if isShuttingDown {
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Check if scheduler is running
	if s.scheduler == nil {
		http.Error(w, "Scheduler not initialized", http.StatusServiceUnavailable)
		return
	}

	// Return healthy status
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"status": "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version": "0.2.0",
		"components": map[string]string{
			"scheduler": "healthy",
			"server": "healthy",
		},
	}
	json.NewEncoder(w).Encode(response)
}

// handleHTTPReadinessCheck handles readiness probe requests
func (s *MCPServer) handleHTTPReadinessCheck(w http.ResponseWriter, r *http.Request) {
	// Check if the server is ready to serve requests
	s.shutdownMutex.Lock()
	isShuttingDown := s.isShuttingDown
	s.shutdownMutex.Unlock()

	if isShuttingDown {
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Check if all components are ready
	isReady := true
	components := make(map[string]string)

	// Check scheduler
	if s.scheduler == nil {
		isReady = false
		components["scheduler"] = "not ready"
	} else {
		components["scheduler"] = "ready"
	}

	// Check MCP server
	if s.server == nil {
		isReady = false
		components["server"] = "not ready"
	} else {
		components["server"] = "ready"
	}

	// Check storage if enabled
	if s.storage != nil {
		// Try a simple operation to verify storage is accessible
		_, err := s.storage.LoadAllTasks()
		if err != nil {
			isReady = false
			components["storage"] = "not ready"
		} else {
			components["storage"] = "ready"
		}
	}

	// Return appropriate status
	status := http.StatusOK
	statusText := "ready"
	if !isReady {
		status = http.StatusServiceUnavailable
		statusText = "not ready"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	response := map[string]interface{}{
		"status": statusText,
		"timestamp": time.Now().Format(time.RFC3339),
		"components": components,
	}
	json.NewEncoder(w).Encode(response)
}
