// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"mcp-cron-persistent/internal/model"
)

type WorkflowExecutor struct {
	dashboardURL string
	httpClient   *http.Client
	mu           sync.RWMutex
	results      map[string]*model.Result
}

func NewWorkflowExecutor(dashboardURL string) *WorkflowExecutor {
	if dashboardURL == "" {
		dashboardURL = os.Getenv("DASHBOARD_INTERNAL_URL")
		if dashboardURL == "" {
			dashboardURL = "http://mcp-compose-dashboard:3001"
		}
	}

	return &WorkflowExecutor{
		dashboardURL: dashboardURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		results: make(map[string]*model.Result),
	}
}

func (we *WorkflowExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	startTime := time.Now()

	if task.WorkflowID == "" {
		return fmt.Errorf("workflow task missing workflowId")
	}

	url := fmt.Sprintf("%s/api/workflows/%s/execute", we.dashboardURL, task.WorkflowID)

	input := map[string]interface{}{
		"taskId":        task.ID,
		"taskName":      task.Name,
		"chatSessionId": task.ChatSessionID,
	}

	body, _ := json.Marshal(map[string]interface{}{"input": input})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		we.storeErrorResult(task.ID, startTime, fmt.Sprintf("failed to create request: %v", err))
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := we.httpClient.Do(req)
	if err != nil {
		we.storeErrorResult(task.ID, startTime, fmt.Sprintf("failed to execute workflow: %v", err))
		return fmt.Errorf("failed to execute workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("workflow execution failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		we.storeErrorResult(task.ID, startTime, errMsg)
		return fmt.Errorf(errMsg)
	}

	var workflowResult WorkflowExecutionResult
	if err := json.NewDecoder(resp.Body).Decode(&workflowResult); err != nil {
		we.storeErrorResult(task.ID, startTime, fmt.Sprintf("failed to decode workflow result: %v", err))
		return fmt.Errorf("failed to decode workflow result: %w", err)
	}

	if workflowResult.Error != "" {
		we.storeErrorResult(task.ID, startTime, fmt.Sprintf("workflow execution error: %s", workflowResult.Error))
		return fmt.Errorf("workflow execution error: %s", workflowResult.Error)
	}

	we.storeSuccessResult(task.ID, startTime, &workflowResult)
	return nil
}

type WorkflowExecutionResult struct {
	ExecutionID string                 `json:"executionId"`
	Status      string                 `json:"status"`
	Output      map[string]interface{} `json:"output"`
	NodeStates  []NodeExecutionState   `json:"nodeStates,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Duration    string                 `json:"duration"`
}

type NodeExecutionState struct {
	NodeID      string                 `json:"node_id"`
	Status      string                 `json:"status"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Output      map[string]interface{} `json:"output,omitempty"`
}

func (we *WorkflowExecutor) storeSuccessResult(taskID string, startTime time.Time, workflowResult *WorkflowExecutionResult) {
	var finalOutput string

	if len(workflowResult.NodeStates) > 0 {
		lastNode := workflowResult.NodeStates[len(workflowResult.NodeStates)-1]

		if lastNode.Output != nil {
			if response, ok := lastNode.Output["response"].(string); ok {
				finalOutput = response
			} else if content, ok := lastNode.Output["content"].(string); ok {
				finalOutput = content
			} else if result, ok := lastNode.Output["result"]; ok {
				if resultStr, isString := result.(string); isString {
					finalOutput = resultStr
				} else {
					outputJSON, _ := json.Marshal(result)
					finalOutput = string(outputJSON)
				}
			} else {
				outputJSON, _ := json.MarshalIndent(lastNode.Output, "", "  ")
				finalOutput = string(outputJSON)
			}
		}
	}

	if finalOutput == "" {
		outputJSON, _ := json.MarshalIndent(workflowResult.Output, "", "  ")
		finalOutput = string(outputJSON)
	}

	result := &model.Result{
		TaskID:    taskID,
		Output:    finalOutput,
		ExitCode:  0,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  time.Since(startTime).String(),
	}

	we.mu.Lock()
	we.results[taskID] = result
	we.mu.Unlock()

	fmt.Printf("[DEBUG] WorkflowExecutor: Stored success result for task %s: %s\n", taskID, result.Output)
}

func (we *WorkflowExecutor) storeErrorResult(taskID string, startTime time.Time, errMsg string) {
	result := &model.Result{
		TaskID:    taskID,
		Output:    errMsg,
		Error:     errMsg,
		ExitCode:  1,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  time.Since(startTime).String(),
	}

	we.mu.Lock()
	we.results[taskID] = result
	we.mu.Unlock()

	fmt.Printf("[DEBUG] WorkflowExecutor: Stored error result for task %s: %s\n", taskID, errMsg)
}

func (we *WorkflowExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	we.mu.RLock()
	defer we.mu.RUnlock()

	result, exists := we.results[taskID]
	return result, exists
}
