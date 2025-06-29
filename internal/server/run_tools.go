// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"mcp-cron-persistent/internal/errors"
	"mcp-cron-persistent/internal/model"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
)

// RunStatusParams holds parameters for run status queries
type RunStatusParams struct {
	TaskID string `json:"task_id,omitempty" description:"filter by specific task ID"`
	Limit  int    `json:"limit,omitempty" description:"limit number of results (default: 10)"`
}

// RunOutputParams holds parameters for run output queries
type RunOutputParams struct {
	TaskID string `json:"task_id" description:"task ID to get output for"`
	RunID  string `json:"run_id,omitempty" description:"specific run ID (optional, gets latest if not specified)"`
}

// RunSearchParams holds parameters for searching runs
type RunSearchParams struct {
	Status   string `json:"status,omitempty" description:"filter by status (completed, failed, running)"`
	TaskName string `json:"task_name,omitempty" description:"filter by task name (partial match)"`
	Since    string `json:"since,omitempty" description:"filter runs since date (YYYY-MM-DD)"`
	Limit    int    `json:"limit,omitempty" description:"limit number of results (default: 20)"`
}

// ExportRunsParams holds parameters for exporting runs
type ExportRunsParams struct {
	Format string `json:"format,omitempty" description:"export format: json, csv, markdown (default: json)"`
	Since  string `json:"since,omitempty" description:"export runs since date (YYYY-MM-DD)"`
	TaskID string `json:"task_id,omitempty" description:"filter by specific task ID"`
}

// RunSummary represents a summary of task runs
type RunSummary struct {
	TaskID     string    `json:"task_id"`
	TaskName   string    `json:"task_name"`
	TaskType   string    `json:"task_type"`
	Status     string    `json:"status"`
	LastRun    time.Time `json:"last_run"`
	NextRun    time.Time `json:"next_run"`
	RunCount   int       `json:"run_count"`
	FailCount  int       `json:"fail_count"`
	LastOutput string    `json:"last_output,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

// RunDetail represents detailed information about a specific run
type RunDetail struct {
	TaskID    string    `json:"task_id"`
	TaskName  string    `json:"task_name"`
	TaskType  string    `json:"task_type"`
	RunID     string    `json:"run_id"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  string    `json:"duration"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	ExitCode  int       `json:"exit_code"`
	Command   string    `json:"command,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
}

// handleListRunStatus lists recent task executions with status
func (s *MCPServer) handleListRunStatus(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params RunStatusParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}

	s.logger.Debugf("Handling list_run_status request, limit: %d", params.Limit)

	// Get all tasks
	allTasks := s.scheduler.ListTasks()
	runSummaries := make([]RunSummary, 0)

	for _, task := range allTasks {
		// Filter by task ID if specified
		if params.TaskID != "" && task.ID != params.TaskID {
			continue
		}

		// Get latest result for this task
		var lastResult *model.Result
		var resultExists bool

		// Try to get result from command executor
		if lastResult, resultExists = s.cmdExecutor.GetTaskResult(task.ID); !resultExists {
			// Try to get result from agent executor
			lastResult, resultExists = s.agentExecutor.GetTaskResult(task.ID)
		}

		summary := RunSummary{
			TaskID:   task.ID,
			TaskName: task.Name,
			TaskType: task.Type,
			Status:   string(task.Status),
			LastRun:  task.LastRun,
			NextRun:  task.NextRun,
		}

		if resultExists && lastResult != nil {
			summary.LastOutput = truncateString(lastResult.Output, 200)
			summary.LastError = lastResult.Error
		}

		runSummaries = append(runSummaries, summary)
	}

	// Sort by last run time, most recent first
	sort.Slice(runSummaries, func(i, j int) bool {
		return runSummaries[i].LastRun.After(runSummaries[j].LastRun)
	})

	// Apply limit
	if len(runSummaries) > params.Limit {
		runSummaries = runSummaries[:params.Limit]
	}

	// Create response
	responseJSON, err := json.Marshal(runSummaries)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal run status: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

// handleGetRunOutput gets detailed output from a specific task run
func (s *MCPServer) handleGetRunOutput(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params RunOutputParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.TaskID == "" {
		return createErrorResponse(errors.InvalidInput("task_id is required"))
	}

	s.logger.Debugf("Handling get_run_output request for task: %s", params.TaskID)

	// Get task details
	task, err := s.scheduler.GetTask(params.TaskID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Get latest result for this task
	var result *model.Result
	var resultExists bool

	// Try to get result from command executor
	if result, resultExists = s.cmdExecutor.GetTaskResult(params.TaskID); !resultExists {
		// Try to get result from agent executor
		result, resultExists = s.agentExecutor.GetTaskResult(params.TaskID)
	}

	if !resultExists {
		return createErrorResponse(errors.NotFound("task result", params.TaskID))
	}

	// Create detailed run information
	runDetail := RunDetail{
		TaskID:    task.ID,
		TaskName:  task.Name,
		TaskType:  task.Type,
		RunID:     fmt.Sprintf("%s_%d", task.ID, result.StartTime.Unix()),
		Status:    string(task.Status),
		StartTime: result.StartTime,
		EndTime:   result.EndTime,
		Duration:  result.Duration,
		Output:    result.Output,
		Error:     result.Error,
		ExitCode:  result.ExitCode,
		Command:   result.Command,
		Prompt:    result.Prompt,
	}

	// Create response
	responseJSON, err := json.Marshal(runDetail)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal run detail: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

// handleSearchRuns searches task runs with filters
func (s *MCPServer) handleSearchRuns(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params RunSearchParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.Limit <= 0 {
		params.Limit = 20
	}

	s.logger.Debugf("Handling search_runs request with filters")

	// Parse since date if provided
	var sinceTime time.Time
	if params.Since != "" {
		var err error
		sinceTime, err = time.Parse("2006-01-02", params.Since)
		if err != nil {
			return createErrorResponse(errors.InvalidInput(fmt.Sprintf("invalid date format for 'since': %s (use YYYY-MM-DD)", params.Since)))
		}
	}

	// Get all tasks
	allTasks := s.scheduler.ListTasks()
	var filteredRuns []RunSummary

	for _, task := range allTasks {
		// Apply filters
		if params.Status != "" && string(task.Status) != params.Status {
			continue
		}

		if params.TaskName != "" && !contains(strings.ToLower(task.Name), strings.ToLower(params.TaskName)) {
			continue
		}

		if !sinceTime.IsZero() && task.LastRun.Before(sinceTime) {
			continue
		}

		// Get result for additional info
		var lastResult *model.Result
		var resultExists bool

		if lastResult, resultExists = s.cmdExecutor.GetTaskResult(task.ID); !resultExists {
			lastResult, resultExists = s.agentExecutor.GetTaskResult(task.ID)
		}

		summary := RunSummary{
			TaskID:   task.ID,
			TaskName: task.Name,
			TaskType: task.Type,
			Status:   string(task.Status),
			LastRun:  task.LastRun,
			NextRun:  task.NextRun,
		}

		if resultExists && lastResult != nil {
			summary.LastOutput = truncateString(lastResult.Output, 200)
			summary.LastError = lastResult.Error
		}

		filteredRuns = append(filteredRuns, summary)
	}

	// Sort by last run time, most recent first
	sort.Slice(filteredRuns, func(i, j int) bool {
		return filteredRuns[i].LastRun.After(filteredRuns[j].LastRun)
	})

	// Apply limit
	if len(filteredRuns) > params.Limit {
		filteredRuns = filteredRuns[:params.Limit]
	}

	// Create response
	responseJSON, err := json.Marshal(filteredRuns)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal search results: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: string(responseJSON),
			},
		},
	}, nil
}

// handleExportRuns exports run data to various formats
func (s *MCPServer) handleExportRuns(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params struct {
		Format string `json:"format,omitempty" description:"export format: json, csv, markdown (default: json)"`
		Since  string `json:"since,omitempty" description:"export runs since date (YYYY-MM-DD)"`
		TaskID string `json:"task_id,omitempty" description:"filter by specific task ID"`
	}

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.Format == "" {
		params.Format = "json"
	}

	s.logger.Debugf("Handling export_runs request, format: %s", params.Format)

	// Get all tasks
	allTasks := s.scheduler.ListTasks()
	var exportData []RunSummary

	// Parse since date if provided
	var sinceTime time.Time
	if params.Since != "" {
		var err error
		sinceTime, err = time.Parse("2006-01-02", params.Since)
		if err != nil {
			return createErrorResponse(errors.InvalidInput(fmt.Sprintf("invalid date format for 'since': %s (use YYYY-MM-DD)", params.Since)))
		}
	}

	for _, task := range allTasks {
		// Apply filters
		if params.TaskID != "" && task.ID != params.TaskID {
			continue
		}

		if !sinceTime.IsZero() && task.LastRun.Before(sinceTime) {
			continue
		}

		// Get result for additional info
		var lastResult *model.Result
		var resultExists bool

		if lastResult, resultExists = s.cmdExecutor.GetTaskResult(task.ID); !resultExists {
			lastResult, resultExists = s.agentExecutor.GetTaskResult(task.ID)
		}

		summary := RunSummary{
			TaskID:   task.ID,
			TaskName: task.Name,
			TaskType: task.Type,
			Status:   string(task.Status),
			LastRun:  task.LastRun,
			NextRun:  task.NextRun,
		}

		if resultExists && lastResult != nil {
			summary.LastOutput = lastResult.Output // Full output for export
			summary.LastError = lastResult.Error   // Fixed: = instead of ==
		}

		exportData = append(exportData, summary)
	}

	// Generate output based on format
	var output string
	var err error

	switch params.Format {
	case "json":
		var jsonBytes []byte
		jsonBytes, err = json.MarshalIndent(exportData, "", "  ")
		output = string(jsonBytes)
	case "csv":
		output, err = generateCSV(exportData)
	case "markdown":
		output, err = generateMarkdown(exportData)
	default:
		return createErrorResponse(errors.InvalidInput(fmt.Sprintf("unsupported format: %s (use json, csv, or markdown)", params.Format)))
	}

	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to generate export: %w", err))
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: "text",
				Text: output,
			},
		},
	}, nil
}

// Helper functions
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(substr) > 0 && len(s) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func generateCSV(data []RunSummary) (string, error) {
	if len(data) == 0 {
		return "task_id,task_name,task_type,status,last_run,next_run\n", nil
	}

	csv := "task_id,task_name,task_type,status,last_run,next_run,last_error\n"
	for _, run := range data {
		csv += fmt.Sprintf("%s,%s,%s,%s,%s,%s,%q\n",
			run.TaskID,
			run.TaskName,
			run.TaskType,
			run.Status,
			run.LastRun.Format("2006-01-02 15:04:05"),
			run.NextRun.Format("2006-01-02 15:04:05"),
			run.LastError,
		)
	}
	return csv, nil
}

func generateMarkdown(data []RunSummary) (string, error) {
	if len(data) == 0 {
		return "# Task Run Report\n\nNo runs found.\n", nil
	}

	md := "# Task Run Report\n\n"
	md += "| Task ID | Name | Type | Status | Last Run | Next Run |\n"
	md += "|---------|------|------|--------|----------|----------|\n"

	for _, run := range data {
		status := run.Status
		if run.LastError != "" {
			status += " ⚠️"
		}

		md += fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			run.TaskID,
			run.TaskName,
			run.TaskType,
			status,
			run.LastRun.Format("2006-01-02 15:04"),
			run.NextRun.Format("2006-01-02 15:04"),
		)
	}

	return md, nil
}
