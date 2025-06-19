// SPDX-License-Identifier: AGPL-3.0-only
package model

import (
	"context"
	"time"
)

// TaskType defines the types of tasks that can be executed
type TaskType string

// TaskStatus represents the current status of a task
type TaskStatus string

// Task types
const (
	TypeShellCommand TaskType = "shell_command"
	TypeAI           TaskType = "AI"
)

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

// String returns the string representation of the type, making it easier to use in string contexts
func (t TaskType) String() string {
	return string(t)
}

// String returns the string representation of the status, making it easier to use in string contexts
func (s TaskStatus) String() string {
	return string(s)
}

// Task represents a scheduled task
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Command     string     `json:"command,omitempty" description:"command for shell"`
	Prompt      string     `json:"prompt,omitempty" description:"prompt to use for AI"`
	Schedule    string     `json:"schedule"`
	Enabled     bool       `json:"enabled"`
	Type        string     `json:"type"`
	LastRun     time.Time  `json:"lastRun,omitempty"`
	NextRun     time.Time  `json:"nextRun,omitempty"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"createdAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt,omitempty"`

	// Conversation and agent support
	ConversationID      string     `json:"conversationId,omitempty" description:"OpenWebUI conversation ID for persistent context"`
	ConversationName    string     `json:"conversationName,omitempty" description:"Human-readable name for the conversation"`
	ConversationContext string     `json:"conversationContext,omitempty" description:"Additional context for the conversation"`
	IsAgent             bool       `json:"isAgent,omitempty" description:"Whether this task represents an autonomous agent"`
	AgentPersonality    string     `json:"agentPersonality,omitempty" description:"Personality/role description for the agent"`
	MemorySummary       string     `json:"memorySummary,omitempty" description:"Summarized memory from previous executions"`
	LastMemoryUpdate    *time.Time `json:"lastMemoryUpdate,omitempty" description:"When memory was last updated"`
}

// Result contains the results of a task execution
type Result struct {
	TaskID         string    `json:"task_id"`
	Command        string    `json:"command,omitempty" description:"command for shell"`
	Prompt         string    `json:"prompt,omitempty" description:"prompt to use for AI"`
	Output         string    `json:"output"`
	Error          string    `json:"error,omitempty"`
	ExitCode       int       `json:"exit_code"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Duration       string    `json:"duration"`
	ConversationID string    `json:"conversation_id,omitempty" description:"OpenWebUI conversation ID used"`
}

// Conversation represents an OpenWebUI conversation
type Conversation struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastUsed    time.Time `json:"last_used"`
	Context     string    `json:"context,omitempty"`
	Type        string    `json:"type,omitempty"` // "task", "agent", "user_initiated"
	Description string    `json:"description,omitempty"`
}

// Executor defines the interface for executing tasks
type Executor interface {
	Execute(ctx context.Context, task *Task, timeout time.Duration) error
}

// ResultProvider defines an interface for getting task execution results
type ResultProvider interface {
	GetTaskResult(taskID string) (*Result, bool)
}
