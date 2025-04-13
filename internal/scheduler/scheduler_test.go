package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// MockTaskExecutor implements the TaskExecutor interface for testing
type MockTaskExecutor struct {
	ExecuteTaskFunc func(task *Task) error
}

// ExecuteTask fulfills the TaskExecutor interface
func (m *MockTaskExecutor) ExecuteTask(task *Task) error {
	if m.ExecuteTaskFunc != nil {
		return m.ExecuteTaskFunc(task)
	}
	return nil
}

func TestNewScheduler(t *testing.T) {
	s := NewScheduler()
	if s == nil {
		t.Fatal("NewScheduler() returned nil")
	}
	if s.cron == nil {
		t.Error("Scheduler.cron is nil")
	}
	if s.tasks == nil {
		t.Error("Scheduler.tasks is nil")
	}
	if s.entryIDs == nil {
		t.Error("Scheduler.entryIDs is nil")
	}
}

func TestAddGetTask(t *testing.T) {
	s := NewScheduler()
	now := time.Now()
	task := &Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
		LastRun:     now,
		NextRun:     now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Get the task
	retrieved, err := s.GetTask("test-task")
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrieved.ID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, retrieved.ID)
	}
	if retrieved.Name != task.Name {
		t.Errorf("Expected task name %s, got %s", task.Name, retrieved.Name)
	}

	// Verify LastRun and NextRun values
	if retrieved.LastRun.IsZero() {
		t.Error("Expected LastRun to be initialized, but it's zero")
	}
	if retrieved.NextRun.IsZero() {
		t.Error("Expected NextRun to be initialized, but it's zero")
	}

	// Verify LastRun and NextRun match the values we set
	if !retrieved.LastRun.Equal(now) {
		t.Errorf("Expected LastRun %v, got %v", now, retrieved.LastRun)
	}
	if !retrieved.NextRun.Equal(now) {
		t.Errorf("Expected NextRun %v, got %v", now, retrieved.NextRun)
	}
}

func TestAddDuplicateTask(t *testing.T) {
	s := NewScheduler()
	task := &Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *",
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Try to add it again
	err = s.AddTask(task)
	if err == nil {
		t.Error("Expected error when adding duplicate task, got nil")
	}
}

func TestListTasks(t *testing.T) {
	s := NewScheduler()
	task1 := &Task{
		ID:      "task1",
		Name:    "Task 1",
		Enabled: false,
	}
	task2 := &Task{
		ID:      "task2",
		Name:    "Task 2",
		Enabled: false,
	}

	// Add tasks
	_ = s.AddTask(task1)
	_ = s.AddTask(task2)

	// List tasks
	tasks := s.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}
}

func TestRemoveTask(t *testing.T) {
	s := NewScheduler()
	task := &Task{
		ID:      "test-task",
		Name:    "Test Task",
		Enabled: false,
	}

	// Add the task
	_ = s.AddTask(task)

	// Remove the task
	err := s.RemoveTask("test-task")
	if err != nil {
		t.Fatalf("Failed to remove task: %v", err)
	}

	// Try to get the task
	_, err = s.GetTask("test-task")
	if err == nil {
		t.Error("Expected error when getting removed task, got nil")
	}
}

func TestUpdateTask(t *testing.T) {
	s := NewScheduler()
	task := &Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *",
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
	}

	// Add the task
	_ = s.AddTask(task)

	// Update the task
	updatedTask := &Task{
		ID:          "test-task",
		Name:        "Updated Task",
		Schedule:    "* * * * * *",
		Command:     "echo updated",
		Description: "An updated test task",
		Enabled:     false,
	}

	err := s.UpdateTask(updatedTask)
	if err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	// Get the updated task
	retrieved, _ := s.GetTask("test-task")
	if retrieved.Name != "Updated Task" {
		t.Errorf("Expected updated name 'Updated Task', got %s", retrieved.Name)
	}
	if retrieved.Command != "echo updated" {
		t.Errorf("Expected updated command 'echo updated', got %s", retrieved.Command)
	}
}

func TestEnableDisableTask(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up a mock executor
	mockExecutor := &MockTaskExecutor{}
	s.SetTaskExecutor(mockExecutor)

	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	task := &Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
	}

	// Add the task
	_ = s.AddTask(task)

	// Enable the task
	err := s.EnableTask("test-task")
	if err != nil {
		t.Fatalf("Failed to enable task: %v", err)
	}

	// Get the task to verify it's enabled
	retrieved, _ := s.GetTask("test-task")
	if !retrieved.Enabled {
		t.Error("Task should be enabled")
	}

	// Disable the task
	err = s.DisableTask("test-task")
	if err != nil {
		t.Fatalf("Failed to disable task: %v", err)
	}

	// Get the task to verify it's disabled
	retrieved, _ = s.GetTask("test-task")
	if retrieved.Enabled {
		t.Error("Task should be disabled")
	}
}

// TestNewTask verifies that NewTask initializes time fields properly
func TestNewTask(t *testing.T) {
	beforeTime := time.Now().Add(-1 * time.Second)
	task := NewTask()

	// Check that CreatedAt and UpdatedAt are initialized
	if task.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be initialized, but it's zero")
	}

	if task.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be initialized, but it's zero")
	}

	// Verify times are recent
	if task.CreatedAt.Before(beforeTime) {
		t.Errorf("Expected CreatedAt to be after %v, but was %v", beforeTime, task.CreatedAt)
	}

	if task.UpdatedAt.Before(beforeTime) {
		t.Errorf("Expected UpdatedAt to be after %v, but was %v", beforeTime, task.UpdatedAt)
	}

	// LastRun and NextRun should be zero in the default case
	// since they will be set when the task is scheduled

	// Check default values
	if task.Enabled != false {
		t.Errorf("Expected Enabled to be false, but was %v", task.Enabled)
	}

	if task.Status != StatusPending.String() {
		t.Errorf("Expected Status to be %q, but was %q", StatusPending, task.Status)
	}
}

// TestCronExpressionSupport confirms that both standard (minute-based) and non-standard (second-based) cron expressions are supported
func TestCronExpressionSupport(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up a mock executor
	mockExecutor := &MockTaskExecutor{}
	s.SetTaskExecutor(mockExecutor)

	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	// Test standard cron expression (every minute)
	standardTask := &Task{
		ID:          "standard-cron-task",
		Name:        "Standard Cron Task",
		Schedule:    "* * * * *", // Run every minute (standard cron format)
		Command:     "echo standard cron",
		Description: "A task using standard cron expression",
		Enabled:     true,
	}

	// Test non-standard cron expression (every second)
	nonStandardTask := &Task{
		ID:          "non-standard-cron-task",
		Name:        "Non-Standard Cron Task",
		Schedule:    "* * * * * *", // Run every second (non-standard cron format with seconds)
		Command:     "echo non-standard cron",
		Description: "A task using non-standard cron expression with seconds",
		Enabled:     true,
	}

	// Add the tasks
	err := s.AddTask(standardTask)
	if err != nil {
		t.Fatalf("Failed to add standard task: %v", err)
	}

	err = s.AddTask(nonStandardTask)
	if err != nil {
		t.Fatalf("Failed to add non-standard task: %v", err)
	}

	// Verify the tasks were added correctly
	standardRetrieved, err := s.GetTask("standard-cron-task")
	if err != nil {
		t.Fatalf("Failed to get standard task: %v", err)
	}
	if standardRetrieved.Schedule != "* * * * *" {
		t.Errorf("Expected standard schedule '* * * * *', got %s", standardRetrieved.Schedule)
	}

	nonStandardRetrieved, err := s.GetTask("non-standard-cron-task")
	if err != nil {
		t.Fatalf("Failed to get non-standard task: %v", err)
	}
	if nonStandardRetrieved.Schedule != "* * * * * *" {
		t.Errorf("Expected non-standard schedule '* * * * * *', got %s", nonStandardRetrieved.Schedule)
	}

	// Verify both tasks are enabled
	if !standardRetrieved.Enabled {
		t.Error("Standard task should be enabled")
	}
	if !nonStandardRetrieved.Enabled {
		t.Error("Non-standard task should be enabled")
	}

	// Verify both tasks have entry IDs (are properly scheduled)
	s.mu.RLock()
	_, standardExists := s.entryIDs[standardTask.ID]
	_, nonStandardExists := s.entryIDs[nonStandardTask.ID]
	s.mu.RUnlock()

	if !standardExists {
		t.Error("Standard task should have an entry ID")
	}
	if !nonStandardExists {
		t.Error("Non-standard task should have an entry ID")
	}
}

// TestTaskExecutorPattern tests the direct execution of tasks using the TaskExecutor interface
func TestTaskExecutorPattern(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up execution tracking
	var taskExecuted bool
	var executedTaskID string

	// Create a mock executor that records execution
	mockExecutor := &MockTaskExecutor{
		ExecuteTaskFunc: func(task *Task) error {
			taskExecuted = true
			executedTaskID = task.ID
			return nil
		},
	}

	// Set the mock executor
	s.SetTaskExecutor(mockExecutor)

	// Start the scheduler
	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	// Create a task that will run immediately
	task := &Task{
		ID:          "test-executor-task",
		Name:        "Test Executor Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo test",
		Description: "A task for testing the executor pattern",
		Enabled:     true,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Wait a moment for the task to execute
	time.Sleep(1500 * time.Millisecond)

	// Verify the task was executed
	if !taskExecuted {
		t.Error("Task was not executed by the TaskExecutor")
	}

	// Verify the right task was executed
	if executedTaskID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, executedTaskID)
	}

	// Test error handling from TaskExecutor
	// Set up a mock executor that returns an error
	errorExecutor := &MockTaskExecutor{
		ExecuteTaskFunc: func(task *Task) error {
			return fmt.Errorf("test error from executor")
		},
	}

	// Reset the scheduler
	err = s.Stop()
	if err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}
	s = NewScheduler()
	s.SetTaskExecutor(errorExecutor)
	s.Start(ctx)

	// Create a new task
	errorTask := &Task{
		ID:          "test-error-task",
		Name:        "Test Error Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo error test",
		Description: "A task for testing error handling",
		Enabled:     true,
	}

	// Add the task
	err = s.AddTask(errorTask)
	if err != nil {
		t.Fatalf("Failed to add error task: %v", err)
	}

	// Wait a moment for the task to execute
	time.Sleep(1500 * time.Millisecond)

	// Get the task to verify its status was set to failed
	retrievedTask, err := s.GetTask(errorTask.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Verify the task status was updated to failed
	if retrievedTask.Status != StatusFailed.String() {
		t.Errorf("Expected status %s, got %s", StatusFailed.String(), retrievedTask.Status)
	}
}

// TestMissingTaskExecutor verifies that the scheduler fails to schedule tasks if no executor is set
func TestMissingTaskExecutor(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	defer func(s *Scheduler) {
		err := s.Stop()
		if err != nil {
			t.Fatalf("Failed to stop scheduler: %v", err)
		}
	}(s)

	// Create a task
	task := &Task{
		ID:          "missing-executor-task",
		Name:        "Missing Executor Task",
		Schedule:    "* * * * * *",
		Command:     "echo test",
		Description: "A task for testing missing executor",
		Enabled:     true,
	}

	// Try to add the task - this should fail because we enabled the task but didn't set an executor
	err := s.AddTask(task)
	if err == nil {
		t.Error("Expected AddTask to fail with no executor, but it succeeded")
	}

	// Now try with disabled task - this should work
	task.ID = "missing-executor-task-2" // Use a different ID
	task.Enabled = false
	err = s.AddTask(task)
	if err != nil {
		t.Errorf("Failed to add disabled task: %v", err)
	}

	// Now try to enable it - this should fail
	err = s.EnableTask(task.ID)
	if err == nil {
		t.Error("Expected EnableTask to fail with no executor, but it succeeded")
	}
}
