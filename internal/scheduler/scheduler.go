// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/robfig/cron/v3"
)

// Storage interface for persistence
type Storage interface {
	SaveTask(task *model.Task) error
	LoadTask(id string) (*model.Task, error)
	LoadAllTasks() ([]*model.Task, error)
	DeleteTask(id string) error
	SaveTaskResult(result *model.Result) error
	Close() error
}

// Scheduler manages cron tasks
type Scheduler struct {
	cron              *cron.Cron
	tasks             map[string]*model.Task
	entryIDs          map[string]cron.EntryID
	mu                sync.RWMutex
	taskExecutor      model.Executor
	config            *config.SchedulerConfig
	storage           Storage
	dependencyManager *DependencyManager
	watcherManager    *WatcherManager
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg *config.SchedulerConfig) *Scheduler {
	cronOpts := cron.New(
		cron.WithParser(cron.NewParser(
			cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		),
	)

	scheduler := &Scheduler{
		cron:     cronOpts,
		tasks:    make(map[string]*model.Task),
		entryIDs: make(map[string]cron.EntryID),
		config:   cfg,
	}

	// Initialize new managers
	scheduler.dependencyManager = NewDependencyManager(scheduler)
	scheduler.watcherManager = NewWatcherManager(scheduler, logging.GetDefaultLogger())

	return scheduler
}

// SetStorage sets the storage backend for persistence
func (s *Scheduler) SetStorage(storage Storage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.storage = storage

	// Load existing tasks from storage
	if storage != nil {
		return s.loadTasksFromStorage()
	}

	return nil
}

// loadTasksFromStorage loads all tasks from persistent storage
func (s *Scheduler) loadTasksFromStorage() error {
	tasks, err := s.storage.LoadAllTasks()
	if err != nil {
		return fmt.Errorf("failed to load tasks from storage: %w", err)
	}

	for _, task := range tasks {
		s.tasks[task.ID] = task

		// Schedule enabled tasks
		if task.Enabled && s.taskExecutor != nil {
			if scheduleErr := s.scheduleTask(task); scheduleErr != nil {
				// Log error but continue loading other tasks
				fmt.Printf("Failed to schedule task %s: %v\n", task.ID, scheduleErr)
				task.Status = model.StatusFailed
				s.saveTaskToStorage(task) // Save the failed status
			}
		}
	}

	return nil
}

// saveTaskToStorage saves a task to persistent storage if available
func (s *Scheduler) saveTaskToStorage(task *model.Task) {
	if s.storage != nil {
		if err := s.storage.SaveTask(task); err != nil {
			fmt.Printf("Failed to save task %s to storage: %v\n", task.ID, err)
		}
	}
}

// Start begins the scheduler
func (s *Scheduler) Start(ctx context.Context) {
	s.cron.Start()

	// Listen for context cancellation to stop the scheduler
	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			fmt.Printf("Error stopping scheduler: %v\n", err)
		}
	}()
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	// Remove the task from cron if it's scheduled
	if entryID, exists := s.entryIDs[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, taskID)
	}

	// Remove from storage
	if s.storage != nil {
		if err := s.storage.DeleteTask(taskID); err != nil {
			return fmt.Errorf("failed to delete task from storage: %w", err)
		}
	}

	// Remove the task from our map
	delete(s.tasks, taskID)

	return nil
}

// GetTask retrieves a task by ID
func (s *Scheduler) GetTask(taskID string) (*model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, errors.NotFound("task", taskID)
	}

	return task, nil
}

// ListTasks returns all tasks
func (s *Scheduler) ListTasks() []*model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*model.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	return tasks
}

// UpdateTask updates an existing task
func (s *Scheduler) UpdateTask(task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingTask, exists := s.tasks[task.ID]
	if !exists {
		return errors.NotFound("task", task.ID)
	}

	// If the task was scheduled, remove it
	if existingTask.Enabled {
		if entryID, exists := s.entryIDs[task.ID]; exists {
			s.cron.Remove(entryID)
			delete(s.entryIDs, task.ID)
		}
	}

	// Update the task
	task.UpdatedAt = time.Now()
	s.tasks[task.ID] = task

	// Save to storage
	s.saveTaskToStorage(task)

	// If enabled, schedule it
	if task.Enabled {
		return s.scheduleTask(task)
	}

	return nil
}

// SetTaskExecutor sets the executor to be used for task execution
func (s *Scheduler) SetTaskExecutor(executor model.Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskExecutor = executor
}

// NewTask creates a new task with default values
func NewTask() *model.Task {
	now := time.Now()
	return &model.Task{
		Enabled:   false,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// scheduleTask adds a task to the cron scheduler (internal method)
func (s *Scheduler) scheduleTask(task *model.Task) error {
	// Ensure we have a task executor
	if s.taskExecutor == nil {
		return fmt.Errorf("cannot schedule task: no task executor set")
	}

	// Create the job function that will execute when scheduled
	jobFunc := func() {
		task.LastRun = time.Now()
		task.Status = model.StatusRunning
		s.saveTaskToStorage(task)

		// Execute the task
		ctx := context.Background()
		timeout := s.config.DefaultTimeout // Use the configured default timeout

		// Create a result to track execution
		result := &model.Result{
			TaskID:    task.ID,
			StartTime: time.Now(),
		}

		if err := s.taskExecutor.Execute(ctx, task, timeout); err != nil {
			task.Status = model.StatusFailed
			result.Error = err.Error()
			result.ExitCode = 1
		} else {
			task.Status = model.StatusCompleted
			result.ExitCode = 0
		}

		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime).String()

		task.UpdatedAt = time.Now()
		s.updateNextRunTime(task)
		s.saveTaskToStorage(task)

		// Save execution result if storage is available
		if s.storage != nil {
			if err := s.storage.SaveTaskResult(result); err != nil {
				fmt.Printf("Failed to save task result: %v\n", err)
			}
		}
	}

	// Add the job to cron
	entryID, err := s.cron.AddFunc(task.Schedule, jobFunc)
	if err != nil {
		return fmt.Errorf("failed to schedule task: %w", err)
	}

	// Store the cron entry ID
	s.entryIDs[task.ID] = entryID
	s.updateNextRunTime(task)

	return nil
}

// updateNextRunTime updates the task's next run time based on its cron entry
func (s *Scheduler) updateNextRunTime(task *model.Task) {
	if entryID, exists := s.entryIDs[task.ID]; exists {
		entries := s.cron.Entries()
		for _, entry := range entries {
			if entry.ID == entryID {
				task.NextRun = entry.Next
				break
			}
		}
	}
}

// TriggerTask manually triggers a task (for dependencies and watchers)
func (s *Scheduler) triggerTask(task *model.Task) {
	// Check if task is enabled
	if !task.Enabled {
		return
	}

	// For dependency tasks, check if dependencies are satisfied
	if task.TriggerType == model.TriggerTypeDependency {
		if satisfied, unsatisfied := s.dependencyManager.CheckDependencies(task); !satisfied {
			// Add to pending executions
			for _, depID := range unsatisfied {
				s.dependencyManager.AddPendingExecution(depID, task.ID)
			}
			return
		}
	}

	// Execute the task immediately
	go s.executeTaskNow(task)
}

// AddTask adds a new task to the scheduler
func (s *Scheduler) AddTask(task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return errors.AlreadyExists("task", task.ID)
	}

	// Set default trigger type if not specified
	if task.TriggerType == "" {
		if task.RunOnDemandOnly {
			task.TriggerType = model.TriggerTypeManual
		} else if len(task.DependsOn) > 0 {
			task.TriggerType = model.TriggerTypeDependency
		} else if task.WatcherConfig != nil {
			task.TriggerType = model.TriggerTypeWatcher
		} else {
			task.TriggerType = model.TriggerTypeSchedule
		}
	}

	// Store the task in memory
	s.tasks[task.ID] = task

	// Save to persistent storage
	s.saveTaskToStorage(task)

	if task.Enabled {
		err := s.handleTaskScheduling(task)
		if err != nil {
			task.Status = model.StatusFailed
			s.saveTaskToStorage(task)
			return err
		}
	}

	return nil
}

// EnableTask enables a disabled task
func (s *Scheduler) EnableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	if task.Enabled {
		return nil // Already enabled
	}

	task.Enabled = true
	task.UpdatedAt = time.Now()

	err := s.handleTaskScheduling(task)
	if err != nil {
		task.Status = model.StatusFailed
		s.saveTaskToStorage(task)
		return err
	}

	s.saveTaskToStorage(task)
	return nil
}

// DisableTask disables a running task
func (s *Scheduler) DisableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	if !task.Enabled {
		return nil // Already disabled
	}

	// Remove from cron if scheduled
	if entryID, exists := s.entryIDs[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, taskID)
	}

	// Stop watcher if it's a watcher task
	if task.TriggerType == model.TriggerTypeWatcher {
		s.watcherManager.StopWatcher(taskID)
	}

	task.Enabled = false
	task.Status = model.StatusDisabled
	task.UpdatedAt = time.Now()
	s.saveTaskToStorage(task)
	return nil
}

// Stop halts the scheduler
func (s *Scheduler) Stop() error {
	s.cron.Stop()
	if s.watcherManager != nil {
		s.watcherManager.Stop()
	}

	// Close storage if available
	if s.storage != nil {
		return s.storage.Close()
	}
	return nil
}

// TriggerTask manually triggers a task (for dependencies and watchers)
func (s *Scheduler) TriggerTask(task *model.Task) {
	// Check if task is enabled
	if !task.Enabled {
		return
	}

	// For dependency tasks, check if dependencies are satisfied
	if task.TriggerType == model.TriggerTypeDependency {
		if satisfied, unsatisfied := s.dependencyManager.CheckDependencies(task); !satisfied {
			// Add to pending executions
			for _, depID := range unsatisfied {
				s.dependencyManager.AddPendingExecution(depID, task.ID)
			}
			return
		}
	}

	// Execute the task immediately
	go s.executeTaskNow(task)
}

// executeTaskNow executes a task immediately
func (s *Scheduler) executeTaskNow(task *model.Task) {
	if s.taskExecutor == nil {
		fmt.Printf("Cannot execute task %s: no task executor set\n", task.ID)
		return
	}

	task.LastRun = time.Now()
	task.Status = model.StatusRunning
	s.saveTaskToStorage(task)

	ctx := context.Background()
	timeout := s.config.DefaultTimeout

	// Execute the task
	if err := s.taskExecutor.Execute(ctx, task, timeout); err != nil {
		task.Status = model.StatusFailed
	} else {
		task.Status = model.StatusCompleted
		// Notify dependency manager of completion
		s.dependencyManager.OnTaskCompleted(task.ID)
	}

	task.UpdatedAt = time.Now()
	s.saveTaskToStorage(task)
}

// handleTaskScheduling handles scheduling based on trigger type
func (s *Scheduler) handleTaskScheduling(task *model.Task) error {
	switch task.TriggerType {
	case model.TriggerTypeSchedule:
		return s.scheduleTask(task)
	case model.TriggerTypeWatcher:
		if s.watcherManager != nil {
			return s.watcherManager.StartWatcher(task)
		}
		return fmt.Errorf("watcher manager not available")
	case model.TriggerTypeDependency:
		// Dependencies are handled when dependency tasks complete
		return nil
	case model.TriggerTypeManual:
		// Manual tasks are only triggered via run_task
		return nil
	default:
		return fmt.Errorf("unknown trigger type: %s", task.TriggerType)
	}
}
