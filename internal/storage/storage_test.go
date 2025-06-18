// SPDX-License-Identifier: AGPL-3.0-only
package storage

import (
	"os"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/model"
)

func TestSQLiteStorage(t *testing.T) {
	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "test_mcp_cron_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create storage
	storage, err := NewSQLiteStorage(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Test task creation
	now := time.Now()
	task := &model.Task{
		ID:          "test-task-1",
		Name:        "Test Task",
		Description: "A test task",
		Command:     "echo hello",
		Schedule:    "*/5 * * * *",
		Enabled:     true,
		Type:        model.TypeShellCommand.String(),
		LastRun:     now,
		NextRun:     now.Add(5 * time.Minute),
		Status:      model.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save task
	err = storage.SaveTask(task)
	if err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Load task
	loadedTask, err := storage.LoadTask("test-task-1")
	if err != nil {
		t.Fatalf("Failed to load task: %v", err)
	}

	// Verify task fields
	if loadedTask.ID != task.ID {
		t.Errorf("Expected ID %s, got %s", task.ID, loadedTask.ID)
	}
	if loadedTask.Name != task.Name {
		t.Errorf("Expected Name %s, got %s", task.Name, loadedTask.Name)
	}
	if loadedTask.Command != task.Command {
		t.Errorf("Expected Command %s, got %s", task.Command, loadedTask.Command)
	}
	if loadedTask.Enabled != task.Enabled {
		t.Errorf("Expected Enabled %v, got %v", task.Enabled, loadedTask.Enabled)
	}

	// Test load all tasks
	tasks, err := storage.LoadAllTasks()
	if err != nil {
		t.Fatalf("Failed to load all tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Test task deletion
	err = storage.DeleteTask("test-task-1")
	if err != nil {
		t.Fatalf("Failed to delete task: %v", err)
	}

	// Verify task is deleted
	_, err = storage.LoadTask("test-task-1")
	if err == nil {
		t.Error("Expected error when loading deleted task")
	}
}

func TestSQLiteStorageResults(t *testing.T) {
	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "test_mcp_cron_results_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create storage
	storage, err := NewSQLiteStorage(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Create and save a task first
	now := time.Now()
	task := &model.Task{
		ID:        "test-task-results",
		Name:      "Test Task for Results",
		Command:   "echo hello",
		Schedule:  "*/5 * * * *",
		Enabled:   true,
		Type:      model.TypeShellCommand.String(),
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err = storage.SaveTask(task)
	if err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Create test result
	result := &model.Result{
		TaskID:    "test-task-results",
		Command:   "echo hello",
		Output:    "hello",
		ExitCode:  0,
		StartTime: now,
		EndTime:   now.Add(time.Second),
		Duration:  "1s",
	}

	// Save result
	err = storage.SaveTaskResult(result)
	if err != nil {
		t.Fatalf("Failed to save task result: %v", err)
	}

	// Load results
	results, err := storage.LoadTaskResults("test-task-results", 10)
	if err != nil {
		t.Fatalf("Failed to load task results: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Verify result
	loadedResult := results[0]
	if loadedResult.TaskID != result.TaskID {
		t.Errorf("Expected TaskID %s, got %s", result.TaskID, loadedResult.TaskID)
	}
	if loadedResult.Output != result.Output {
		t.Errorf("Expected Output %s, got %s", result.Output, loadedResult.Output)
	}
	if loadedResult.ExitCode != result.ExitCode {
		t.Errorf("Expected ExitCode %d, got %d", result.ExitCode, loadedResult.ExitCode)
	}
}
