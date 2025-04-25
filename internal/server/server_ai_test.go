// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"testing"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

// createTestServer creates a minimal MCPServer for testing
func createTestServer(t *testing.T) *MCPServer {
	// Create dependencies
	sched := scheduler.NewScheduler()
	cmdExec := command.NewCommandExecutor()

	// Create a config for testing
	cfg := config.DefaultConfig()

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
func TestHandleAddAITask(t *testing.T) {
	server := createTestServer(t)

	// Test case 1: Valid AI task with minimum fields
	minimalRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Minimal AI Task",
			"schedule": "* * * * *",
			"prompt": "Minimal prompt"
		}`),
	}

	result, err := server.handleAddAITask(minimalRequest)
	if err != nil {
		t.Fatalf("handleAddAITask failed for minimal request: %v", err)
	}
	if result == nil {
		t.Fatal("handleAddAITask returned nil result for minimal request")
	}

	// Test case 2: AI task with all fields
	fullRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Full AI Task",
			"schedule": "0 */5 * * * *",
			"prompt": "Full prompt with details",
			"description": "Test AI task with all fields",
			"type": "AI",
			"enabled": true
		}`),
	}

	result, err = server.handleAddAITask(fullRequest)
	if err != nil {
		t.Fatalf("handleAddAITask failed for full request: %v", err)
	}
	if result == nil {
		t.Fatal("handleAddAITask returned nil result for full request")
	}

	// Test case 3: Invalid request (missing required fields)
	invalidRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Invalid Task"
		}`),
	}

	_, err = server.handleAddAITask(invalidRequest)
	if err == nil {
		t.Fatal("handleAddAITask should fail for request missing required fields")
	}

	// Test case 4: Malformed JSON
	malformedRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Malformed Task",
			"schedule": "* * * * *"
			"prompt": "Malformed prompt"
		}`),
	}

	_, err = server.handleAddAITask(malformedRequest)
	if err == nil {
		t.Fatal("handleAddAITask should fail for malformed JSON")
	}

	// Verify the correct tasks were created
	tasks := server.scheduler.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}

	// Verify properties of created tasks
	var minimalTask, fullTask *model.Task
	for _, task := range tasks {
		if task.Name == "Minimal AI Task" {
			minimalTask = task
		} else if task.Name == "Full AI Task" {
			fullTask = task
		}
	}

	// Check minimal task
	if minimalTask == nil {
		t.Fatal("Minimal task not found")
	}
	if minimalTask.Type != model.TypeAI.String() {
		t.Errorf("Expected minimal task type to be %s, got %s", model.TypeAI.String(), minimalTask.Type)
	}
	if minimalTask.Prompt != "Minimal prompt" {
		t.Errorf("Expected minimal task prompt to be 'Minimal prompt', got '%s'", minimalTask.Prompt)
	}
	if minimalTask.Schedule != "* * * * *" {
		t.Errorf("Expected minimal task schedule to be '* * * * *', got '%s'", minimalTask.Schedule)
	}

	// Check full task
	if fullTask == nil {
		t.Fatal("Full task not found")
	}
	if fullTask.Type != model.TypeAI.String() {
		t.Errorf("Expected full task type to be %s, got %s", model.TypeAI.String(), fullTask.Type)
	}
	if fullTask.Prompt != "Full prompt with details" {
		t.Errorf("Expected full task prompt to be 'Full prompt with details', got '%s'", fullTask.Prompt)
	}
	if fullTask.Description != "Test AI task with all fields" {
		t.Errorf("Expected full task description to be 'Test AI task with all fields', got '%s'", fullTask.Description)
	}
	if fullTask.Schedule != "0 */5 * * * *" {
		t.Errorf("Expected full task schedule to be '0 */5 * * * *', got '%s'", fullTask.Schedule)
	}
	if !fullTask.Enabled {
		t.Error("Expected full task to be enabled")
	}
}

// TestUpdateAITask tests updating AI tasks
func TestUpdateAITask(t *testing.T) {
	server := createTestServer(t)

	// First, create an AI task to update
	createRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "AI Task to Update",
			"schedule": "* * * * *",
			"prompt": "Original prompt",
			"description": "Original description",
			"enabled": true
		}`),
	}

	_, err := server.handleAddAITask(createRequest)
	if err != nil {
		t.Fatalf("Failed to create AI task: %v", err)
	}

	// Get the created task
	tasks := server.scheduler.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}
	originalTask := tasks[0]

	// Test case 1: Update prompt
	promptUpdateRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"id": "` + originalTask.ID + `",
			"prompt": "Updated prompt"
		}`),
	}

	_, err = server.handleUpdateTask(promptUpdateRequest)
	if err != nil {
		t.Fatalf("Failed to update prompt: %v", err)
	}

	// Verify prompt was updated
	updatedTask, err := server.scheduler.GetTask(originalTask.ID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}
	if updatedTask.Prompt != "Updated prompt" {
		t.Errorf("Expected prompt to be 'Updated prompt', got '%s'", updatedTask.Prompt)
	}

	// Test case 2: Update multiple fields
	multiUpdateRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"id": "` + originalTask.ID + `",
			"name": "New Name",
			"description": "New description",
			"schedule": "0 0 * * *",
			"enabled": false
		}`),
	}

	_, err = server.handleUpdateTask(multiUpdateRequest)
	if err != nil {
		t.Fatalf("Failed to update multiple fields: %v", err)
	}

	// Verify multiple fields were updated
	updatedTask, err = server.scheduler.GetTask(originalTask.ID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}
	if updatedTask.Name != "New Name" {
		t.Errorf("Expected name to be 'New Name', got '%s'", updatedTask.Name)
	}
	if updatedTask.Description != "New description" {
		t.Errorf("Expected description to be 'New description', got '%s'", updatedTask.Description)
	}
	if updatedTask.Schedule != "0 0 * * *" {
		t.Errorf("Expected schedule to be '0 0 * * *', got '%s'", updatedTask.Schedule)
	}
	if updatedTask.Enabled {
		t.Error("Expected task to be disabled")
	}

	// Test case 3: Invalid task ID
	invalidIDRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"id": "non-existent-task",
			"prompt": "This shouldn't work"
		}`),
	}

	_, err = server.handleUpdateTask(invalidIDRequest)
	if err == nil {
		t.Fatal("Update with invalid task ID should fail")
	}
}

// TestConvertTaskTypes tests converting between task types
func TestConvertTaskTypes(t *testing.T) {
	server := createTestServer(t)

	// Create a shell command task
	shellTaskRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Shell Task",
			"schedule": "* * * * *",
			"command": "echo test",
			"enabled": true
		}`),
	}

	_, err := server.handleAddTask(shellTaskRequest)
	if err != nil {
		t.Fatalf("Failed to create shell task: %v", err)
	}

	// Get the created task
	tasks := server.scheduler.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}
	shellTask := tasks[0]

	// Convert to AI task
	convertRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"id": "` + shellTask.ID + `",
			"type": "AI",
			"prompt": "Converted task prompt"
		}`),
	}

	_, err = server.handleUpdateTask(convertRequest)
	if err != nil {
		t.Fatalf("Failed to convert task type: %v", err)
	}

	// Verify conversion
	convertedTask, err := server.scheduler.GetTask(shellTask.ID)
	if err != nil {
		t.Fatalf("Failed to get converted task: %v", err)
	}
	if convertedTask.Type != model.TypeAI.String() {
		t.Errorf("Expected task type to be %s, got %s", model.TypeAI.String(), convertedTask.Type)
	}
	if convertedTask.Prompt != "Converted task prompt" {
		t.Errorf("Expected prompt to be 'Converted task prompt', got '%s'", convertedTask.Prompt)
	}

	// Convert back to shell command
	convertBackRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"id": "` + shellTask.ID + `",
			"type": "shell_command",
			"command": "echo converted back"
		}`),
	}

	_, err = server.handleUpdateTask(convertBackRequest)
	if err != nil {
		t.Fatalf("Failed to convert task back: %v", err)
	}

	// Verify conversion back
	reconvertedTask, err := server.scheduler.GetTask(shellTask.ID)
	if err != nil {
		t.Fatalf("Failed to get reconverted task: %v", err)
	}
	if reconvertedTask.Type != model.TypeShellCommand.String() {
		t.Errorf("Expected task type to be %s, got %s", model.TypeShellCommand.String(), reconvertedTask.Type)
	}
	if reconvertedTask.Command != "echo converted back" {
		t.Errorf("Expected command to be 'echo converted back', got '%s'", reconvertedTask.Command)
	}
}
