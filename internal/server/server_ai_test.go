// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

// createTestServer creates a minimal MCPServer for testing
func createAITestServer(_ *testing.T) *MCPServer {
	// Create a config for testing
	cfg := config.DefaultConfig()

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler)
	cmdExec := command.NewCommandExecutor()
	agentExec := agent.NewAgentExecutor(cfg)

	// Create a logger
	logger := logging.New(logging.Options{
		Level: logging.Info,
	})

	// Create server
	server := &MCPServer{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		logger:        logger,
		config:        cfg,
	}

	// Set the server as task executor (required for scheduling)
	sched.SetTaskExecutor(server)

	return server
}

// TestHandleAddAITask tests the AI task creation handler
func TestHandleAddAITask_AI(t *testing.T) {
	server := createAITestServer(t)

	// Test case 1: Valid AI task with minimum fields
	validTask := AITaskParams{
		Name:     "AI Test Task",
		Schedule: "*/5 * * * *",
		Prompt:   "Generate a report on system health",
		Enabled:  true,
	}

	// Create the request with the valid task
	validRequestJSON, _ := json.Marshal(validTask)
	validRequest := &protocol.CallToolRequest{
		RawArguments: validRequestJSON,
	}

	// Call the handler
	result, err := server.handleAddAITask(validRequest)
	if err != nil {
		t.Fatalf("Valid request should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid request")
	}

	// Test case 2: Missing required fields (prompt)
	invalidTask := AITaskParams{
		Name:     "Invalid AI Task",
		Schedule: "*/5 * * * *",
		// Missing prompt
	}

	invalidRequestJSON, _ := json.Marshal(invalidTask)
	invalidRequest := &protocol.CallToolRequest{
		RawArguments: invalidRequestJSON,
	}

	// Call the handler
	result, err = server.handleAddAITask(invalidRequest)
	if err == nil {
		t.Fatal("Invalid request should fail")
	}
	if result != nil {
		t.Fatal("Result should be nil for invalid request")
	}

	// Test case 3: Missing required fields (name)
	missingNameTask := AITaskParams{
		Schedule: "*/5 * * * *",
		Prompt:   "Generate a report on system health",
	}

	missingNameRequestJSON, _ := json.Marshal(missingNameTask)
	missingNameRequest := &protocol.CallToolRequest{
		RawArguments: missingNameRequestJSON,
	}

	// Call the handler
	result, err = server.handleAddAITask(missingNameRequest)
	if err == nil {
		t.Fatal("Request with missing name should fail")
	}
	if result != nil {
		t.Fatal("Result should be nil for request with missing name")
	}

	// Test case 4: Missing required fields (schedule)
	missingScheduleTask := AITaskParams{
		Name:   "Missing Schedule Task",
		Prompt: "Generate a report on system health",
	}

	missingScheduleRequestJSON, _ := json.Marshal(missingScheduleTask)
	missingScheduleRequest := &protocol.CallToolRequest{
		RawArguments: missingScheduleRequestJSON,
	}

	// Call the handler
	result, err = server.handleAddAITask(missingScheduleRequest)
	if err == nil {
		t.Fatal("Request with missing schedule should fail")
	}
	if result != nil {
		t.Fatal("Result should be nil for request with missing schedule")
	}
}

// TestUpdateAITask tests updating AI tasks
func TestUpdateAITask_AI(t *testing.T) {
	server := createAITestServer(t)

	// First, create an AI task to update
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	initialTask := &model.Task{
		ID:          taskID,
		Name:        "Initial AI Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeAI.String(),
		Prompt:      "Initial prompt",
		Description: "Initial description",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err := server.scheduler.AddTask(initialTask)
	if err != nil {
		t.Fatalf("Failed to add initial task: %v", err)
	}

	// Test case 1: Update AI task prompt
	updateParams := AITaskParams{
		ID:     taskID,
		Prompt: "Updated prompt",
	}

	updateRequestJSON, _ := json.Marshal(updateParams)
	updateRequest := &protocol.CallToolRequest{
		RawArguments: updateRequestJSON,
	}

	// Call the handler
	result, err := server.handleUpdateTask(updateRequest)
	if err != nil {
		t.Fatalf("Valid update should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid update")
	}

	// Verify the task was updated
	updatedTask, err := server.scheduler.GetTask(taskID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}
	if updatedTask.Prompt != "Updated prompt" {
		t.Errorf("Expected prompt to be updated to 'Updated prompt', got '%s'", updatedTask.Prompt)
	}

	// Test case 2: Update multiple fields
	multiUpdateParams := AITaskParams{
		ID:          taskID,
		Name:        "Updated AI Task",
		Schedule:    "*/10 * * * *",
		Description: "Updated description",
		Enabled:     true,
	}

	multiUpdateRequestJSON, _ := json.Marshal(multiUpdateParams)
	multiUpdateRequest := &protocol.CallToolRequest{
		RawArguments: multiUpdateRequestJSON,
	}

	// Call the handler
	result, err = server.handleUpdateTask(multiUpdateRequest)
	if err != nil {
		t.Fatalf("Valid multi-update should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid multi-update")
	}

	// Verify the task was updated
	updatedTask, err = server.scheduler.GetTask(taskID)
	if err != nil {
		t.Fatalf("Failed to get multi-updated task: %v", err)
	}
	if updatedTask.Name != "Updated AI Task" {
		t.Errorf("Expected name to be updated to 'Updated AI Task', got '%s'", updatedTask.Name)
	}
	if updatedTask.Schedule != "*/10 * * * *" {
		t.Errorf("Expected schedule to be updated to '*/10 * * * *', got '%s'", updatedTask.Schedule)
	}
	if updatedTask.Description != "Updated description" {
		t.Errorf("Expected description to be updated to 'Updated description', got '%s'", updatedTask.Description)
	}
	if !updatedTask.Enabled {
		t.Error("Expected task to be enabled after update")
	}
}

// TestConvertTaskTypes tests converting between task types
func TestConvertTaskTypes_AI(t *testing.T) {
	server := createAITestServer(t)

	// Create a shell command task
	shellTaskID := fmt.Sprintf("shell_task_%d", time.Now().UnixNano())
	shellTask := &model.Task{
		ID:          shellTaskID,
		Name:        "Shell Command Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeShellCommand.String(),
		Command:     "echo hello",
		Description: "A shell command task",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err := server.scheduler.AddTask(shellTask)
	if err != nil {
		t.Fatalf("Failed to add shell task: %v", err)
	}

	// Convert shell command task to AI task
	updateParams := AITaskParams{
		ID:     shellTaskID,
		Type:   model.TypeAI.String(),
		Prompt: "New AI prompt",
	}

	updateRequestJSON, _ := json.Marshal(updateParams)
	updateRequest := &protocol.CallToolRequest{
		RawArguments: updateRequestJSON,
	}

	// Call the handler
	result, err := server.handleUpdateTask(updateRequest)
	if err != nil {
		t.Fatalf("Valid conversion should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid conversion")
	}

	// Verify the task was converted
	convertedTask, err := server.scheduler.GetTask(shellTaskID)
	if err != nil {
		t.Fatalf("Failed to get converted task: %v", err)
	}
	if convertedTask.Type != model.TypeAI.String() {
		t.Errorf("Expected type to be converted to '%s', got '%s'", model.TypeAI.String(), convertedTask.Type)
	}
	if convertedTask.Prompt != "New AI prompt" {
		t.Errorf("Expected prompt to be set to 'New AI prompt', got '%s'", convertedTask.Prompt)
	}

	// Create an AI task
	aiTaskID := fmt.Sprintf("ai_task_%d", time.Now().UnixNano())
	aiTask := &model.Task{
		ID:          aiTaskID,
		Name:        "AI Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeAI.String(),
		Prompt:      "AI prompt",
		Description: "An AI task",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err = server.scheduler.AddTask(aiTask)
	if err != nil {
		t.Fatalf("Failed to add AI task: %v", err)
	}

	// Convert AI task to shell command task
	convertParams := AITaskParams{
		ID:      aiTaskID,
		Type:    model.TypeShellCommand.String(),
		Command: "echo converted",
	}

	convertRequestJSON, _ := json.Marshal(convertParams)
	convertRequest := &protocol.CallToolRequest{
		RawArguments: convertRequestJSON,
	}

	// Call the handler
	result, err = server.handleUpdateTask(convertRequest)
	if err != nil {
		t.Fatalf("Valid conversion should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid conversion")
	}

	// Verify the task was converted
	reconvertedTask, err := server.scheduler.GetTask(aiTaskID)
	if err != nil {
		t.Fatalf("Failed to get reconverted task: %v", err)
	}
	if reconvertedTask.Type != model.TypeShellCommand.String() {
		t.Errorf("Expected type to be converted to '%s', got '%s'", model.TypeShellCommand.String(), reconvertedTask.Type)
	}
	if reconvertedTask.Command != "echo converted" {
		t.Errorf("Expected command to be set to 'echo converted', got '%s'", reconvertedTask.Command)
	}
}
