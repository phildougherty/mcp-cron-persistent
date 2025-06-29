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

// Task represents a scheduled task with enhanced scheduling features
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
	ConversationID      string         `json:"conversationId,omitempty" description:"OpenWebUI conversation ID for persistent context"`
	ConversationName    string         `json:"conversationName,omitempty" description:"Human-readable name for the conversation"`
	ConversationContext string         `json:"conversationContext,omitempty" description:"Additional context for the conversation"`
	IsAgent             bool           `json:"isAgent,omitempty" description:"Whether this task represents an autonomous agent"`
	AgentPersonality    string         `json:"agentPersonality,omitempty" description:"Personality/role description for the agent"`
	MemorySummary       string         `json:"memorySummary,omitempty" description:"Summarized memory from previous executions"`
	LastMemoryUpdate    *time.Time     `json:"lastMemoryUpdate,omitempty" description:"When memory was last updated"`
	DependsOn           []string       `json:"dependsOn,omitempty" description:"task IDs this task depends on"`
	TriggerType         string         `json:"triggerType,omitempty" description:"trigger type: schedule, dependency, watcher, manual"`
	WatcherConfig       *WatcherConfig `json:"watcherConfig,omitempty" description:"configuration for watcher tasks"`
	RunOnDemandOnly     bool           `json:"runOnDemandOnly,omitempty" description:"task only runs when manually triggered"`

	// Enhanced scheduling fields
	Timezone         string        `json:"timezone,omitempty" description:"IANA timezone for task execution"`
	SkipHolidays     bool          `json:"skipHolidays,omitempty" description:"skip execution on holidays"`
	HolidayRegion    string        `json:"holidayRegion,omitempty" description:"holiday region (US, UK, etc.)"`
	TimeWindowID     string        `json:"timeWindowId,omitempty" description:"ID of time window constraint"`
	MaxExecutionTime time.Duration `json:"maxExecutionTime,omitempty" description:"maximum allowed execution time"`
	RetryPolicy      *RetryPolicy  `json:"retryPolicy,omitempty" description:"retry configuration"`

	// AI workflow enhancement
	MCPMemoryEnabled bool   `json:"mcpMemoryEnabled,omitempty" description:"enable MCP memory integration"`
	WorkflowContext  string `json:"workflowContext,omitempty" description:"workflow context for AI tasks"`
	LearningEnabled  bool   `json:"learningEnabled,omitempty" description:"enable learning from execution history"`

	// Performance and monitoring
	SLATarget      time.Duration     `json:"slaTarget,omitempty" description:"SLA target for execution time"`
	AlertOnFailure bool              `json:"alertOnFailure,omitempty" description:"send alerts on task failure"`
	MetricsLabels  map[string]string `json:"metricsLabels,omitempty" description:"custom metrics labels"`
}

// WatcherConfig defines what a watcher task should monitor
type WatcherConfig struct {
	Type          string     `json:"type" description:"watcher type: file_creation, task_completion, file_change"`
	WatchPath     string     `json:"watchPath,omitempty" description:"path to watch for file watchers"`
	FilePattern   string     `json:"filePattern,omitempty" description:"file pattern to match"`
	WatchTaskIDs  []string   `json:"watchTaskIDs,omitempty" description:"task IDs to watch for completion"`
	CheckInterval string     `json:"checkInterval,omitempty" description:"how often to check (e.g., '30s', '1m')"`
	TriggerOnce   bool       `json:"triggerOnce" description:"only trigger once per condition"`
	LastTriggered *time.Time `json:"lastTriggered,omitempty" description:"when this watcher last triggered"`
	Description   string     `json:"description,omitempty" description:"additional description for the watcher"`
	CustomConfig  string     `json:"customConfig,omitempty" description:"custom configuration as JSON string"`
}

// RetryPolicy defines retry behavior for failed tasks
type RetryPolicy struct {
	MaxRetries    int           `json:"maxRetries" description:"maximum number of retry attempts"`
	InitialDelay  time.Duration `json:"initialDelay" description:"initial delay before first retry"`
	BackoffFactor float64       `json:"backoffFactor" description:"multiplier for delay between retries"`
	MaxDelay      time.Duration `json:"maxDelay" description:"maximum delay between retries"`
}

// TimeWindow represents a scheduled time window constraint
type TimeWindow struct {
	Start    string `json:"start" description:"start time in HH:MM format"`
	End      string `json:"end" description:"end time in HH:MM format"`
	Timezone string `json:"timezone" description:"IANA timezone for the time window"`
	Days     []int  `json:"days" description:"allowed days of week (0=Sunday, 1=Monday, etc.)"`
}

// MaintenanceWindow represents a maintenance period
type MaintenanceWindow struct {
	ID          string    `json:"id" description:"unique identifier for the maintenance window"`
	Name        string    `json:"name" description:"human-readable name for the maintenance window"`
	Start       time.Time `json:"start" description:"start time of maintenance window"`
	End         time.Time `json:"end" description:"end time of maintenance window"`
	Timezone    string    `json:"timezone" description:"IANA timezone for the maintenance window"`
	Description string    `json:"description" description:"description of maintenance activities"`
	Enabled     bool      `json:"enabled" description:"whether the maintenance window is active"`
	Recurring   bool      `json:"recurring" description:"whether the maintenance window repeats"`
	Pattern     string    `json:"pattern,omitempty" description:"cron pattern for recurring maintenance"`
}

// TaskTriggerType constants
const (
	TriggerTypeSchedule   = "schedule"
	TriggerTypeDependency = "dependency"
	TriggerTypeWatcher    = "watcher"
	TriggerTypeManual     = "manual"
)

// WatcherType constants
const (
	WatcherTypeFileCreation   = "file_creation"
	WatcherTypeFileChange     = "file_change"
	WatcherTypeTaskCompletion = "task_completion"
)

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

// WorkflowStep represents a step in an AI workflow
type WorkflowStep struct {
	ID           string                 `json:"id" description:"unique identifier for the step"`
	Name         string                 `json:"name" description:"human-readable name for the step"`
	Type         string                 `json:"type" description:"step type: ai_task, shell_command, condition, loop"`
	Prompt       string                 `json:"prompt,omitempty" description:"AI prompt for ai_task steps"`
	Command      string                 `json:"command,omitempty" description:"shell command for shell_command steps"`
	Condition    string                 `json:"condition,omitempty" description:"condition expression for condition steps"`
	Dependencies []string               `json:"dependencies,omitempty" description:"step IDs this step depends on"`
	Timeout      time.Duration          `json:"timeout,omitempty" description:"maximum execution time for this step"`
	RetryPolicy  *RetryPolicy           `json:"retryPolicy,omitempty" description:"retry configuration for this step"`
	Parameters   map[string]interface{} `json:"parameters,omitempty" description:"parameters for step execution"`
	OnSuccess    []string               `json:"onSuccess,omitempty" description:"step IDs to execute on success"`
	OnFailure    []string               `json:"onFailure,omitempty" description:"step IDs to execute on failure"`
}

// AIWorkflow represents a complex AI workflow
type AIWorkflow struct {
	ID          string                 `json:"id" description:"unique identifier for the workflow"`
	Name        string                 `json:"name" description:"human-readable name for the workflow"`
	Description string                 `json:"description" description:"description of the workflow"`
	Steps       []WorkflowStep         `json:"steps" description:"workflow steps"`
	StartStep   string                 `json:"startStep" description:"ID of the first step to execute"`
	Variables   map[string]interface{} `json:"variables,omitempty" description:"workflow variables"`
	CreatedAt   time.Time              `json:"createdAt" description:"when the workflow was created"`
	UpdatedAt   time.Time              `json:"updatedAt" description:"when the workflow was last updated"`
}

// Executor defines the interface for executing tasks
type Executor interface {
	Execute(ctx context.Context, task *Task, timeout time.Duration) error
}

// ResultProvider defines an interface for getting task execution results
type ResultProvider interface {
	GetTaskResult(taskID string) (*Result, bool)
}
