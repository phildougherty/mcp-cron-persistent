// internal/scheduler/watcher_manager.go

package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
)

// WatcherManager handles file watchers and other trigger-based tasks
type WatcherManager struct {
	mu        sync.RWMutex
	watchers  map[string]*ActiveWatcher
	scheduler *Scheduler
	logger    *logging.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// ActiveWatcher represents a running watcher
type ActiveWatcher struct {
	TaskID     string
	Config     *model.WatcherConfig
	LastCheck  time.Time
	StopCh     chan struct{}
	IsRunning  bool
	FileStates map[string]os.FileInfo // for file change detection
}

// NewWatcherManager creates a new watcher manager
func NewWatcherManager(scheduler *Scheduler, logger *logging.Logger) *WatcherManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &WatcherManager{
		watchers:  make(map[string]*ActiveWatcher),
		scheduler: scheduler,
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// StartWatcher starts a watcher for a task
func (wm *WatcherManager) StartWatcher(task *model.Task) error {
	if task.WatcherConfig == nil {
		return fmt.Errorf("task %s has no watcher configuration", task.ID)
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Stop existing watcher if any
	if existing, exists := wm.watchers[task.ID]; exists {
		wm.stopWatcher(existing)
	}

	// Create new watcher
	watcher := &ActiveWatcher{
		TaskID:     task.ID,
		Config:     task.WatcherConfig,
		LastCheck:  time.Now(),
		StopCh:     make(chan struct{}),
		IsRunning:  false,
		FileStates: make(map[string]os.FileInfo),
	}

	wm.watchers[task.ID] = watcher

	// Start the watcher goroutine
	go wm.runWatcher(watcher, task)

	wm.logger.Infof("Started watcher for task %s (type: %s)", task.ID, task.WatcherConfig.Type)
	return nil
}

// StopWatcher stops a watcher for a task
func (wm *WatcherManager) StopWatcher(taskID string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if watcher, exists := wm.watchers[taskID]; exists {
		wm.stopWatcher(watcher)
		delete(wm.watchers, taskID)
		wm.logger.Infof("Stopped watcher for task %s", taskID)
	}
}

// stopWatcher stops an individual watcher (must be called with lock held)
func (wm *WatcherManager) stopWatcher(watcher *ActiveWatcher) {
	if watcher.IsRunning {
		close(watcher.StopCh)
		watcher.IsRunning = false
	}
}

// Stop stops all watchers
func (wm *WatcherManager) Stop() {
	wm.cancel()
	wm.mu.Lock()
	defer wm.mu.Unlock()

	for _, watcher := range wm.watchers {
		wm.stopWatcher(watcher)
	}
	wm.watchers = make(map[string]*ActiveWatcher)
}

// runWatcher runs the main watcher loop
func (wm *WatcherManager) runWatcher(watcher *ActiveWatcher, task *model.Task) {
	watcher.IsRunning = true
	defer func() { watcher.IsRunning = false }()

	// Parse check interval
	checkInterval, err := time.ParseDuration(watcher.Config.CheckInterval)
	if err != nil {
		checkInterval = 30 * time.Second // default
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-watcher.StopCh:
			return
		case <-wm.ctx.Done():
			return
		case <-ticker.C:
			if wm.checkWatcherCondition(watcher, task) {
				wm.triggerWatcherTask(watcher, task)

				// If it's a trigger-once watcher, stop after first trigger
				if watcher.Config.TriggerOnce {
					now := time.Now()
					watcher.Config.LastTriggered = &now
					// Update the task in scheduler
					if err := wm.scheduler.UpdateTask(task); err != nil {
						wm.logger.Errorf("Failed to update task after watcher trigger: %v", err)
					}
					return
				}
			}
		}
	}
}

// checkWatcherCondition checks if the watcher's condition is met
func (wm *WatcherManager) checkWatcherCondition(watcher *ActiveWatcher, _ *model.Task) bool {
	switch watcher.Config.Type {
	case model.WatcherTypeFileCreation:
		return wm.checkFileCreation(watcher)
	case model.WatcherTypeFileChange:
		return wm.checkFileChange(watcher)
	case model.WatcherTypeTaskCompletion:
		return wm.checkTaskCompletion(watcher)
	default:
		wm.logger.Errorf("Unknown watcher type: %s", watcher.Config.Type)
		return false
	}
}

// checkFileCreation checks for new files matching the pattern
func (wm *WatcherManager) checkFileCreation(watcher *ActiveWatcher) bool {
	if watcher.Config.WatchPath == "" {
		return false
	}

	// Check if the watch path exists
	if _, err := os.Stat(watcher.Config.WatchPath); os.IsNotExist(err) {
		return false
	}

	// Get pattern regex if specified
	var pattern *regexp.Regexp
	if watcher.Config.FilePattern != "" {
		var err error
		pattern, err = regexp.Compile(watcher.Config.FilePattern)
		if err != nil {
			wm.logger.Errorf("Invalid file pattern for watcher %s: %v", watcher.TaskID, err)
			return false
		}
	}

	// Walk the directory looking for new files
	var foundNew bool
	err := filepath.Walk(watcher.Config.WatchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check pattern if specified
		if pattern != nil && !pattern.MatchString(info.Name()) {
			return nil
		}

		// Check if this is a new file (created after last check)
		if info.ModTime().After(watcher.LastCheck) {
			foundNew = true
			wm.logger.Debugf("Watcher %s detected new file: %s", watcher.TaskID, path)
		}

		return nil
	})

	if err != nil {
		wm.logger.Errorf("Error walking watch path for watcher %s: %v", watcher.TaskID, err)
	}

	watcher.LastCheck = time.Now()
	return foundNew
}

// checkFileChange checks for file modifications
func (wm *WatcherManager) checkFileChange(watcher *ActiveWatcher) bool {
	if watcher.Config.WatchPath == "" {
		return false
	}

	var changedFiles bool

	err := filepath.Walk(watcher.Config.WatchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check pattern if specified
		if watcher.Config.FilePattern != "" {
			pattern, err := regexp.Compile(watcher.Config.FilePattern)
			if err != nil {
				return nil
			}
			if !pattern.MatchString(info.Name()) {
				return nil
			}
		}

		// Compare with stored file state
		if lastInfo, exists := watcher.FileStates[path]; exists {
			// Check if modification time or size changed
			if !info.ModTime().Equal(lastInfo.ModTime()) || info.Size() != lastInfo.Size() {
				changedFiles = true
				wm.logger.Debugf("Watcher %s detected file change: %s", watcher.TaskID, path)
			}
		}

		// Update stored state
		watcher.FileStates[path] = info
		return nil
	})

	if err != nil {
		wm.logger.Errorf("Error checking file changes for watcher %s: %v", watcher.TaskID, err)
	}

	return changedFiles
}

// checkTaskCompletion checks if watched tasks have completed
func (wm *WatcherManager) checkTaskCompletion(watcher *ActiveWatcher) bool {
	if len(watcher.Config.WatchTaskIDs) == 0 {
		return false
	}

	for _, taskID := range watcher.Config.WatchTaskIDs {
		if task, err := wm.scheduler.GetTask(taskID); err == nil {
			// Check if task completed since last check
			if task.Status == model.StatusCompleted && task.LastRun.After(watcher.LastCheck) {
				wm.logger.Debugf("Watcher %s detected task completion: %s", watcher.TaskID, taskID)
				return true
			}
		}
	}

	return false
}

// triggerWatcherTask triggers the watcher task
func (wm *WatcherManager) triggerWatcherTask(_ *ActiveWatcher, task *model.Task) {
	wm.logger.Infof("Triggering watcher task %s (%s)", task.ID, task.Name)
	wm.scheduler.TriggerTask(task) // Fixed to use exported method
}
