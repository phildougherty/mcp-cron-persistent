// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"context"
	"fmt"
	"mcp-cron-persistent/internal/activity"
	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/errors"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
	"mcp-cron-persistent/internal/observability"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Storage interface for persistence
type Storage interface {
	SaveTask(task *model.Task) error
	LoadTask(id string) (*model.Task, error)
	LoadAllTasks() ([]*model.Task, error)
	DeleteTask(id string) error
	SaveTaskResult(result *model.Result) error
	RecordTaskRun(ctx context.Context, runID, taskID string, startTime, endTime time.Time, output, errorMsg string, exitCode int, status, trigger string) error
	Close() error
}

// HolidayProvider interface for holiday checking
type HolidayProvider interface {
	IsHoliday(date time.Time, timezone string) (bool, string, error)
	GetHolidaysInRange(start, end time.Time, timezone string) ([]Holiday, error)
}

// Holiday represents a holiday
type Holiday struct {
	Name     string    `json:"name"`
	Date     time.Time `json:"date"`
	Type     string    `json:"type"` // "national", "regional", "company"
	Timezone string    `json:"timezone"`
}

// Scheduler manages cron tasks with enhanced scheduling features
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

	// Enhanced features
	holidayProvider    HolidayProvider
	maintenanceWindows map[string]*model.MaintenanceWindow
	timeWindows        map[string]*model.TimeWindow
	timezoneCache      map[string]*time.Location
	metricsCollector   *observability.MetricsCollector
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

		// Enhanced features
		maintenanceWindows: make(map[string]*model.MaintenanceWindow),
		timeWindows:        make(map[string]*model.TimeWindow),
		timezoneCache:      make(map[string]*time.Location),
	}

	// Initialize managers
	scheduler.dependencyManager = NewDependencyManager(scheduler)
	scheduler.watcherManager = NewWatcherManager(scheduler, logging.GetDefaultLogger())

	return scheduler
}

// SetMetricsCollector sets the metrics collector
func (s *Scheduler) SetMetricsCollector(collector *observability.MetricsCollector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsCollector = collector
}

// SetHolidayProvider sets the holiday provider
func (s *Scheduler) SetHolidayProvider(provider HolidayProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holidayProvider = provider
}

// AddMaintenanceWindow adds a maintenance window
func (s *Scheduler) AddMaintenanceWindow(window *model.MaintenanceWindow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if window.ID == "" {
		window.ID = fmt.Sprintf("maint_%d", time.Now().UnixNano())
	}

	// Validate timezone
	if _, err := s.getLocation(window.Timezone); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid timezone: %s", window.Timezone))
	}

	s.maintenanceWindows[window.ID] = window
	return nil
}

// AddTimeWindow adds a time window constraint
func (s *Scheduler) AddTimeWindow(id string, window *model.TimeWindow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate timezone
	if _, err := s.getLocation(window.Timezone); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid timezone: %s", window.Timezone))
	}

	// Validate time format
	if _, err := time.Parse("15:04", window.Start); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid start time format: %s", window.Start))
	}
	if _, err := time.Parse("15:04", window.End); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid end time format: %s", window.End))
	}

	s.timeWindows[id] = window
	return nil
}

// getLocation returns timezone location with caching
func (s *Scheduler) getLocation(timezone string) (*time.Location, error) {
	if timezone == "" {
		timezone = "UTC"
	}

	if loc, exists := s.timezoneCache[timezone]; exists {
		return loc, nil
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}

	s.timezoneCache[timezone] = loc
	return loc, nil
}

// ShouldSkipExecution checks if task execution should be skipped
func (s *Scheduler) ShouldSkipExecution(task *model.Task, execTime time.Time) (bool, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	taskTimezone := task.Timezone
	if taskTimezone == "" {
		taskTimezone = "UTC"
	}

	loc, err := s.getLocation(taskTimezone)
	if err != nil {
		return true, fmt.Sprintf("invalid timezone: %s", taskTimezone), err
	}

	localTime := execTime.In(loc)

	// Check maintenance windows
	for _, window := range s.maintenanceWindows {
		if !window.Enabled {
			continue
		}

		windowLoc, err := s.getLocation(window.Timezone)
		if err != nil {
			continue
		}

		windowStart := window.Start.In(windowLoc)
		windowEnd := window.End.In(windowLoc)

		if localTime.After(windowStart) && localTime.Before(windowEnd) {
			return true, fmt.Sprintf("maintenance window: %s", window.Name), nil
		}
	}

	// Check time windows
	if task.TimeWindowID != "" {
		if window, exists := s.timeWindows[task.TimeWindowID]; exists {
			if !s.isWithinTimeWindow(localTime, window) {
				return true, "outside allowed time window", nil
			}
		}
	}

	// Check holidays
	if task.SkipHolidays && s.holidayProvider != nil {
		isHoliday, holidayName, err := s.holidayProvider.IsHoliday(localTime, taskTimezone)
		if err != nil {
			return false, "", err
		}
		if isHoliday {
			return true, fmt.Sprintf("holiday: %s", holidayName), nil
		}
	}

	return false, "", nil
}

// isWithinTimeWindow checks if time is within allowed window
func (s *Scheduler) isWithinTimeWindow(t time.Time, window *model.TimeWindow) bool {
	// Check day of week
	if len(window.Days) > 0 {
		dayMatch := false
		for _, day := range window.Days {
			if int(t.Weekday()) == day {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
	}

	// Check time range
	timeStr := t.Format("15:04")
	return timeStr >= window.Start && timeStr <= window.End
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
	if s.storage != nil {
		task, err := s.storage.LoadTask(taskID)
		if err == nil {
			s.mu.Lock()
			s.tasks[taskID] = task
			s.mu.Unlock()
			return task, nil
		}
	}

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

	// Create the enhanced job function
	jobFunc := func() {
		s.executeTaskWithObservability(task)
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

// executeTaskWithObservability executes a task with enhanced observability and constraints
func (s *Scheduler) executeTaskWithObservability(task *model.Task) {
	startTime := time.Now()

	// Broadcast task start
	activity.BroadcastActivity("INFO", "task",
		fmt.Sprintf("Task '%s' started (scheduled)", task.Name),
		map[string]interface{}{
			"taskId":    task.ID,
			"taskName":  task.Name,
			"taskType":  task.Type,
			"trigger":   "scheduled",
			"schedule":  task.Schedule,
			"startTime": startTime,
		})

	// Check if execution should be skipped
	skip, reason, err := s.ShouldSkipExecution(task, startTime)
	if err != nil {
		activity.BroadcastActivity("ERROR", "task",
			fmt.Sprintf("Task '%s' failed skip check: %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"error":    err.Error(),
				"trigger":  "scheduled",
			})
		if s.metricsCollector != nil {
			s.metricsCollector.RecordTaskExecution(task, time.Since(startTime), err)
		}
		return
	}
	if skip {
		activity.BroadcastActivity("INFO", "task",
			fmt.Sprintf("Task '%s' skipped: %s", task.Name, reason),
			map[string]interface{}{
				"taskId":     task.ID,
				"taskName":   task.Name,
				"skipReason": reason,
				"trigger":    "scheduled",
			})
		logging.GetDefaultLogger().WithField("task_id", task.ID).
			Infof("Skipping task execution: %s", reason)
		return
	}

	// Update task status
	task.LastRun = startTime
	task.Status = model.StatusRunning
	s.saveTaskToStorage(task)

	// Execute the task
	ctx := context.Background()
	timeout := s.config.DefaultTimeout
	if task.MaxExecutionTime > 0 {
		timeout = task.MaxExecutionTime
	}

	// Create a result to track execution
	result := &model.Result{
		TaskID:    task.ID,
		StartTime: startTime,
	}

	var execErr error
	if err := s.taskExecutor.Execute(ctx, task, timeout); err != nil {
		task.Status = model.StatusFailed
		result.Error = err.Error()
		result.ExitCode = 1
		execErr = err

		// Broadcast task failure
		duration := time.Since(startTime)
		activity.BroadcastActivity("ERROR", "task",
			fmt.Sprintf("Task '%s' failed: %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"taskType": task.Type,
				"error":    err.Error(),
				"duration": duration.Seconds(),
				"trigger":  "scheduled",
				"exitCode": 1,
			})
	} else {
		task.Status = model.StatusCompleted
		result.ExitCode = 0

		// Broadcast task success
		duration := time.Since(startTime)
		activity.BroadcastActivity("INFO", "task",
			fmt.Sprintf("Task '%s' completed successfully in %v", task.Name, duration),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"taskType": task.Type,
				"duration": duration.Seconds(),
				"status":   "completed",
				"trigger":  "scheduled",
				"exitCode": 0,
			})
	}

	if resultProvider, ok := s.taskExecutor.(model.ResultProvider); ok {
		if executorResult, found := resultProvider.GetTaskResult(task.ID); found && executorResult != nil {
			fmt.Printf("[DEBUG] Got result from executor: Output=%q\n", executorResult.Output)
			result.Output = executorResult.Output
			result.Command = executorResult.Command
			result.Prompt = executorResult.Prompt
			if executorResult.Error != "" {
				result.Error = executorResult.Error
			}
		} else {
			fmt.Printf("[DEBUG] No result found from executor for task %s\n", task.ID)
		}
	} else {
		fmt.Printf("[DEBUG] taskExecutor does not implement ResultProvider\n")
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	task.UpdatedAt = time.Now()
	s.updateNextRunTime(task)
	s.saveTaskToStorage(task)

	fmt.Printf("[DEBUG] About to save result: TaskID=%s, Output=%q\n", result.TaskID, result.Output)

	// Save execution result if storage is available
	if s.storage != nil {
		if err := s.storage.SaveTaskResult(result); err != nil {
			fmt.Printf("Failed to save task result: %v\n", err)
		}
	}

	// Update metrics
	if s.metricsCollector != nil {
		duration := time.Since(startTime)
		s.metricsCollector.RecordTaskExecution(task, duration, execErr)
	}
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

// triggerTask manually triggers a task (for dependencies and watchers)
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

			activity.BroadcastActivity("ERROR", "task_management",
				fmt.Sprintf("Failed to schedule task '%s': %s", task.Name, err.Error()),
				map[string]interface{}{
					"taskId":   task.ID,
					"taskName": task.Name,
					"taskType": task.Type,
					"error":    err.Error(),
				})
			return err
		}
	}

	// Note: Don't broadcast here as it's already handled in the server handlers
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

		activity.BroadcastActivity("ERROR", "task_management",
			fmt.Sprintf("Failed to enable task '%s': %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   taskID,
				"taskName": task.Name,
				"error":    err.Error(),
			})
		return err
	}

	s.saveTaskToStorage(task)
	// Note: Don't broadcast here as it's handled in the server handlers
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

	// Note: Don't broadcast here as it's handled in the server handlers
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
// executeTaskNow executes a task immediately
func (s *Scheduler) executeTaskNow(task *model.Task) {
	if s.taskExecutor == nil {
		activity.BroadcastActivity("ERROR", "task",
			fmt.Sprintf("Cannot execute task '%s': no task executor set", task.Name),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"error":    "no task executor set",
				"trigger":  "immediate",
			})
		fmt.Printf("Cannot execute task %s: no task executor set\n", task.ID)
		return
	}

	startTime := time.Now()

	// Determine trigger type for broadcasting
	triggerType := "immediate"
	if task.TriggerType == model.TriggerTypeDependency {
		triggerType = "dependency"
	} else if task.TriggerType == model.TriggerTypeWatcher {
		triggerType = "watcher"
	}

	// Broadcast task start
	activity.BroadcastActivity("INFO", "task",
		fmt.Sprintf("Task '%s' started (%s)", task.Name, triggerType),
		map[string]interface{}{
			"taskId":    task.ID,
			"taskName":  task.Name,
			"taskType":  task.Type,
			"trigger":   triggerType,
			"startTime": startTime,
		})

	task.LastRun = startTime
	task.Status = model.StatusRunning
	s.saveTaskToStorage(task)

	ctx := context.Background()
	timeout := s.config.DefaultTimeout
	if task.MaxExecutionTime > 0 {
		timeout = task.MaxExecutionTime
	}

	// Execute the task
	if err := s.taskExecutor.Execute(ctx, task, timeout); err != nil {
		task.Status = model.StatusFailed

		// Broadcast task failure
		duration := time.Since(startTime)
		activity.BroadcastActivity("ERROR", "task",
			fmt.Sprintf("Task '%s' failed: %s", task.Name, err.Error()),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"taskType": task.Type,
				"error":    err.Error(),
				"duration": duration.Seconds(),
				"trigger":  triggerType,
			})
	} else {
		task.Status = model.StatusCompleted

		// Broadcast task success
		duration := time.Since(startTime)
		activity.BroadcastActivity("INFO", "task",
			fmt.Sprintf("Task '%s' completed successfully in %v", task.Name, duration),
			map[string]interface{}{
				"taskId":   task.ID,
				"taskName": task.Name,
				"taskType": task.Type,
				"duration": duration.Seconds(),
				"status":   "completed",
				"trigger":  triggerType,
			})

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
