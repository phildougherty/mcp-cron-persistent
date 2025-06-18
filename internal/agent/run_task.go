// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/openwebui"
)

// RunTask executes an AI task using OpenWebUI
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s via OpenWebUI", t.Name)

	if !cfg.OpenWebUI.Enabled {
		return "", fmt.Errorf("OpenWebUI integration is disabled")
	}

	// Create OpenWebUI client
	client := openwebui.NewClient(&cfg.OpenWebUI, logger)

	// Execute the AI task via OpenWebUI
	result, err := client.ExecuteAITask(
		ctx,
		t.ID,
		t.Prompt,
		cfg.OpenWebUI.Model,
		cfg.OpenWebUI.UserID,
	)

	if err != nil {
		logger.Errorf("Failed to execute AI task via OpenWebUI: %v", err)
		return "", err
	}

	logger.Infof("AI task completed successfully via OpenWebUI")
	return result, nil
}
