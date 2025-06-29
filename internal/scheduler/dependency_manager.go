// internal/scheduler/dependency_manager.go

package scheduler

import (
	"sync"
	"time"

	"mcp-cron-persistent/internal/model"
)

// DependencyManager handles task dependencies and execution chains
type DependencyManager struct {
	mu                sync.RWMutex
	completedTasks    map[string]time.Time // taskID -> completion time
	pendingExecutions map[string][]string  // taskID -> list of dependent task IDs waiting
	scheduler         *Scheduler
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(scheduler *Scheduler) *DependencyManager {
	return &DependencyManager{
		completedTasks:    make(map[string]time.Time),
		pendingExecutions: make(map[string][]string),
		scheduler:         scheduler,
	}
}

// CheckDependencies verifies if a task's dependencies are satisfied
func (dm *DependencyManager) CheckDependencies(task *model.Task) (bool, []string) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if len(task.DependsOn) == 0 {
		return true, nil
	}

	var unsatisfied []string
	for _, depTaskID := range task.DependsOn {
		if _, completed := dm.completedTasks[depTaskID]; !completed {
			unsatisfied = append(unsatisfied, depTaskID)
		}
	}

	return len(unsatisfied) == 0, unsatisfied
}

// OnTaskCompleted notifies the dependency manager that a task has completed
func (dm *DependencyManager) OnTaskCompleted(taskID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Mark task as completed
	dm.completedTasks[taskID] = time.Now()

	// Check for pending tasks that depend on this one
	if dependents, exists := dm.pendingExecutions[taskID]; exists {
		for _, dependentID := range dependents {
			if task, err := dm.scheduler.GetTask(dependentID); err == nil {
				// Check if all dependencies are now satisfied
				if satisfied, _ := dm.CheckDependencies(task); satisfied {
					// Trigger the dependent task
					dm.scheduler.triggerTask(task)
				}
			}
		}
		// Clear the pending list for this task
		delete(dm.pendingExecutions, taskID)
	}
}

// AddPendingExecution registers a task as waiting for dependency completion
func (dm *DependencyManager) AddPendingExecution(dependencyTaskID, waitingTaskID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.pendingExecutions[dependencyTaskID] == nil {
		dm.pendingExecutions[dependencyTaskID] = make([]string, 0)
	}
	dm.pendingExecutions[dependencyTaskID] = append(dm.pendingExecutions[dependencyTaskID], waitingTaskID)
}

// ResetCompletionStatus clears completion status (useful for testing or manual resets)
func (dm *DependencyManager) ResetCompletionStatus(taskID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	delete(dm.completedTasks, taskID)
}
