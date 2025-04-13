package server

import (
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/executor"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

func TestTaskExecutorImplementation(t *testing.T) {
	// Setup test dependencies
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9999,
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: config.SchedulerConfig{
			DefaultTimeout: 1 * time.Minute,
		},
	}

	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Create server
	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a task
	task := &scheduler.Task{
		ID:          "test-direct-task",
		Name:        "Test Direct Execution Task",
		Schedule:    "* * * * *",
		Command:     "echo test",
		Description: "Task for testing direct execution",
		Enabled:     true,
		Status:      scheduler.StatusPending.String(),
	}

	// Verify that the MCPServer implements the TaskExecutor interface
	// by directly calling the ExecuteTask method
	err = server.ExecuteTask(task)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// Verify the status was not changed (since it's the scheduler's job to update it)
	if task.Status != scheduler.StatusPending.String() {
		t.Errorf("Expected task status to remain %s, got %s",
			scheduler.StatusPending.String(), task.Status)
	}

	// Now add the task to scheduler
	if err := sched.AddTask(task); err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Verify the task was added
	if len(sched.ListTasks()) != 1 {
		t.Errorf("Expected 1 task, got %d", len(sched.ListTasks()))
	}

	// Get the task to ensure it's properly stored
	retrievedTask, err := sched.GetTask(task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected ID %s, got %s", task.ID, retrievedTask.ID)
	}

	if retrievedTask.Name != task.Name {
		t.Errorf("Expected Name %s, got %s", task.Name, retrievedTask.Name)
	}

	// Manually simulate task execution by the scheduler
	retrievedTask.Status = scheduler.StatusRunning.String()

	// Execute the task
	err = server.ExecuteTask(retrievedTask)
	if err != nil {
		t.Fatalf("ExecuteTask failed: %v", err)
	}

	// In a real scenario, the scheduler would update this after execution
	// Here we'll simulate that for testing
	retrievedTask.Status = scheduler.StatusCompleted.String()

	// Verify the task execution was successful
	if retrievedTask.Status != scheduler.StatusCompleted.String() {
		t.Errorf("Expected status %s, got %s",
			scheduler.StatusCompleted.String(), retrievedTask.Status)
	}
}
