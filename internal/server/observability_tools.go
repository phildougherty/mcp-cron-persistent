// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"encoding/json"
	"time"

	"mcp-cron-persistent/internal/errors"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
)

// MetricsParams holds parameters for metrics queries
type MetricsParams struct {
	TaskID string `json:"task_id,omitempty" description:"filter by specific task ID"`
	Format string `json:"format,omitempty" description:"output format: json, summary (default: summary)"`
	Since  string `json:"since,omitempty" description:"show metrics since date (YYYY-MM-DD)"`
}

// HealthCheckParams holds parameters for health checks
type HealthCheckParams struct {
	Component string `json:"component,omitempty" description:"specific component to check (scheduler, storage, system)"`
}

// handleGetMetrics returns system and task metrics
func (s *MCPServer) handleGetMetrics(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params MetricsParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.Format == "" {
		params.Format = "summary"
	}

	s.logger.Debugf("Handling get_metrics request, format: %s", params.Format)

	var response interface{}

	if params.TaskID != "" {
		// Get metrics for specific task
		metrics := s.metricsCollector.GetTaskMetrics(params.TaskID)
		if metrics == nil {
			return createErrorResponse(errors.NotFound("task metrics", params.TaskID))
		}
		response = metrics
	} else {
		// Get overall metrics summary
		if params.Format == "json" {
			response = s.metricsCollector.GetAllTaskMetrics()
		} else {
			response = s.metricsCollector.GetMetricsSummary()
		}
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return createErrorResponse(errors.Internal(err))
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

// handleHealthCheck performs health checks and returns status
func (s *MCPServer) handleHealthCheck(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	var params HealthCheckParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling health_check request for component: %s", params.Component)

	// Perform fresh health checks
	schedulerHealthy := s.scheduler != nil
	storageHealthy := s.storage != nil

	// Test storage if available
	if storageHealthy && s.storage != nil {
		// Try to ping storage
		_, err := s.storage.LoadAllTasks()
		storageHealthy = err == nil
	}

	s.metricsCollector.PerformHealthChecks(schedulerHealthy, storageHealthy)

	var response interface{}

	if params.Component != "" {
		// Get health for specific component
		healthChecks := s.metricsCollector.GetHealthChecks()
		if health, exists := healthChecks[params.Component]; exists {
			response = health
		} else {
			return createErrorResponse(errors.NotFound("health check component", params.Component))
		}
	} else {
		// Get all health checks with overall status
		healthChecks := s.metricsCollector.GetHealthChecks()

		// Calculate overall health
		overallStatus := "healthy"
		unhealthyCount := 0
		degradedCount := 0

		for _, health := range healthChecks {
			switch health.Status {
			case "unhealthy":
				unhealthyCount++
			case "degraded":
				degradedCount++
			}
		}

		if unhealthyCount > 0 {
			overallStatus = "unhealthy"
		} else if degradedCount > 0 {
			overallStatus = "degraded"
		}

		response = map[string]interface{}{
			"overall_status": overallStatus,
			"summary": map[string]int{
				"total_components": len(healthChecks),
				"healthy":          len(healthChecks) - unhealthyCount - degradedCount,
				"degraded":         degradedCount,
				"unhealthy":        unhealthyCount,
			},
			"components": healthChecks,
			"timestamp":  time.Now().Format(time.RFC3339),
		}
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return createErrorResponse(errors.Internal(err))
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

// handleGetSystemMetrics returns detailed system performance metrics
func (s *MCPServer) handleGetSystemMetrics(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	s.logger.Debugf("Handling get_system_metrics request")

	// Force update system metrics
	s.metricsCollector.UpdateSystemMetrics()

	systemMetrics := s.metricsCollector.GetSystemMetrics()

	responseJSON, err := json.MarshalIndent(systemMetrics, "", "  ")
	if err != nil {
		return createErrorResponse(errors.Internal(err))
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
