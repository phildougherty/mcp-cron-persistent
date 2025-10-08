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
	"time"

	"mcp-cron-persistent/internal/model"
)

type WorkflowExecutor struct {
	dashboardURL string
	httpClient   *http.Client
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
	}
}

func (we *WorkflowExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
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
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := we.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute workflow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workflow execution failed with status %d: %s", resp.StatusCode, body)
	}

	var result WorkflowExecutionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode workflow result: %w", err)
	}

	if result.Error != "" {
		return fmt.Errorf("workflow execution error: %s", result.Error)
	}

	return nil
}

type WorkflowExecutionResult struct {
	ExecutionID string                 `json:"execution_id"`
	Status      string                 `json:"status"`
	Output      map[string]interface{} `json:"output"`
	Error       string                 `json:"error,omitempty"`
	Duration    string                 `json:"duration"`
}
