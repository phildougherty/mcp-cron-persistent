// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

// TaskStatus represents the current status of a task
type TaskStatus string

// Task status constants
const (
	// StatusPending indicates a task that has not been run yet
	StatusPending TaskStatus = "pending"
	// StatusRunning indicates a task that is currently running
	StatusRunning TaskStatus = "running"
	// StatusCompleted indicates a task that has successfully completed
	StatusCompleted TaskStatus = "completed"
	// StatusFailed indicates a task that has failed
	StatusFailed TaskStatus = "failed"
	// StatusDisabled indicates a task that is disabled
	StatusDisabled TaskStatus = "disabled"
)

// String returns the string representation of the status, making it easier to use in string contexts
func (s TaskStatus) String() string {
	return string(s)
}
