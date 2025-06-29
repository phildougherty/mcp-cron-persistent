// SPDX-License-Identifier: AGPL-3.0-only
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
)

// MetricsCollector collects and stores metrics about task execution
type MetricsCollector struct {
	mu             sync.RWMutex
	taskExecutions map[string]*ExecutionMetrics
	systemMetrics  *SystemMetrics
	healthChecks   map[string]*HealthStatus
	logger         *logging.Logger
	startTime      time.Time
}

// ExecutionMetrics tracks metrics for a specific task
type ExecutionMetrics struct {
	TaskID               string            `json:"task_id"`
	TaskName             string            `json:"task_name"`
	TaskType             string            `json:"task_type"`
	TotalExecutions      int64             `json:"total_executions"`
	SuccessfulExecutions int64             `json:"successful_executions"`
	FailedExecutions     int64             `json:"failed_executions"`
	AverageExecutionTime time.Duration     `json:"average_execution_time"`
	LastExecutionTime    time.Duration     `json:"last_execution_time"`
	LastExecuted         time.Time         `json:"last_executed"`
	LastStatus           model.TaskStatus  `json:"last_status"`
	ErrorRate            float64           `json:"error_rate"`
	ExecutionHistory     []ExecutionRecord `json:"execution_history"`
}

// ExecutionRecord represents a single task execution record
type ExecutionRecord struct {
	Timestamp    time.Time     `json:"timestamp"`
	Duration     time.Duration `json:"duration"`
	Status       string        `json:"status"`
	ErrorMessage string        `json:"error_message,omitempty"`
}

// SystemMetrics tracks system-level performance
type SystemMetrics struct {
	CPUUsagePercent    float64       `json:"cpu_usage_percent"`
	MemoryUsedBytes    uint64        `json:"memory_used_bytes"`
	MemoryTotalBytes   uint64        `json:"memory_total_bytes"`
	MemoryUsagePercent float64       `json:"memory_usage_percent"`
	GoroutineCount     int           `json:"goroutine_count"`
	Uptime             time.Duration `json:"uptime"`
	LastUpdated        time.Time     `json:"last_updated"`
}

// HealthStatus represents the health of a system component
type HealthStatus struct {
	Component   string    `json:"component"`
	Status      string    `json:"status"` // healthy, degraded, unhealthy
	Message     string    `json:"message"`
	LastChecked time.Time `json:"last_checked"`
	CheckCount  int64     `json:"check_count"`
}

// MetricsSummary provides an overall system summary
type MetricsSummary struct {
	SystemMetrics    *SystemMetrics           `json:"system_metrics"`
	TaskCount        int                      `json:"task_count"`
	ActiveTasks      int                      `json:"active_tasks"`
	TotalExecutions  int64                    `json:"total_executions"`
	SuccessRate      float64                  `json:"success_rate"`
	AverageExecTime  time.Duration            `json:"average_execution_time"`
	RecentExecutions []ExecutionRecord        `json:"recent_executions"`
	HealthChecks     map[string]*HealthStatus `json:"health_checks"`
	TopExecutors     []*ExecutionMetrics      `json:"top_executors"`
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *logging.Logger) *MetricsCollector {
	return &MetricsCollector{
		taskExecutions: make(map[string]*ExecutionMetrics),
		systemMetrics:  &SystemMetrics{},
		healthChecks:   make(map[string]*HealthStatus),
		logger:         logger,
		startTime:      time.Now(),
	}
}

// RecordTaskExecution records the execution of a task
func (mc *MetricsCollector) RecordTaskExecution(task *model.Task, duration time.Duration, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics, exists := mc.taskExecutions[task.ID]
	if !exists {
		metrics = &ExecutionMetrics{
			TaskID:           task.ID,
			TaskName:         task.Name,
			TaskType:         task.Type,
			ExecutionHistory: make([]ExecutionRecord, 0, 100), // Keep last 100 executions
		}
		mc.taskExecutions[task.ID] = metrics
	}

	// Update execution counts
	metrics.TotalExecutions++

	status := "success"
	errorMsg := ""
	if err != nil {
		metrics.FailedExecutions++
		status = "failed"
		errorMsg = err.Error()
	} else {
		metrics.SuccessfulExecutions++
	}

	// Update timing
	metrics.LastExecutionTime = duration
	metrics.LastExecuted = time.Now()
	metrics.LastStatus = task.Status

	// Calculate average execution time
	if metrics.TotalExecutions > 0 {
		totalTime := metrics.AverageExecutionTime * time.Duration(metrics.TotalExecutions-1)
		metrics.AverageExecutionTime = (totalTime + duration) / time.Duration(metrics.TotalExecutions)
	}

	// Calculate error rate
	if metrics.TotalExecutions > 0 {
		metrics.ErrorRate = float64(metrics.FailedExecutions) / float64(metrics.TotalExecutions) * 100
	}

	// Add to execution history (keep last 100)
	record := ExecutionRecord{
		Timestamp:    time.Now(),
		Duration:     duration,
		Status:       status,
		ErrorMessage: errorMsg,
	}

	metrics.ExecutionHistory = append(metrics.ExecutionHistory, record)
	if len(metrics.ExecutionHistory) > 100 {
		metrics.ExecutionHistory = metrics.ExecutionHistory[1:]
	}

	mc.logger.Debugf("Recorded execution for task %s: duration=%v, status=%s", task.ID, duration, status)
}

// GetTaskMetrics returns metrics for a specific task
func (mc *MetricsCollector) GetTaskMetrics(taskID string) *ExecutionMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if metrics, exists := mc.taskExecutions[taskID]; exists {
		// Return a copy to avoid race conditions
		metricsCopy := *metrics
		metricsCopy.ExecutionHistory = make([]ExecutionRecord, len(metrics.ExecutionHistory))
		copy(metricsCopy.ExecutionHistory, metrics.ExecutionHistory)
		return &metricsCopy
	}
	return nil
}

// GetAllTaskMetrics returns metrics for all tasks
func (mc *MetricsCollector) GetAllTaskMetrics() map[string]*ExecutionMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]*ExecutionMetrics)
	for taskID, metrics := range mc.taskExecutions {
		metricsCopy := *metrics
		metricsCopy.ExecutionHistory = make([]ExecutionRecord, len(metrics.ExecutionHistory))
		copy(metricsCopy.ExecutionHistory, metrics.ExecutionHistory)
		result[taskID] = &metricsCopy
	}
	return result
}

// UpdateSystemMetrics collects current system metrics
func (mc *MetricsCollector) UpdateSystemMetrics() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)

	mc.systemMetrics.MemoryUsedBytes = memStats.Alloc
	mc.systemMetrics.MemoryTotalBytes = memStats.Sys
	mc.systemMetrics.GoroutineCount = runtime.NumGoroutine()
	mc.systemMetrics.Uptime = time.Since(mc.startTime)
	mc.systemMetrics.LastUpdated = time.Now()

	if mc.systemMetrics.MemoryTotalBytes > 0 {
		mc.systemMetrics.MemoryUsagePercent = float64(mc.systemMetrics.MemoryUsedBytes) / float64(mc.systemMetrics.MemoryTotalBytes) * 100
	}

	// Simple CPU usage estimation based on goroutine count (naive approach)
	// In production, you'd want to use a proper CPU monitoring library
	mc.systemMetrics.CPUUsagePercent = float64(mc.systemMetrics.GoroutineCount) * 0.5
	if mc.systemMetrics.CPUUsagePercent > 100 {
		mc.systemMetrics.CPUUsagePercent = 100
	}
}

// GetSystemMetrics returns current system metrics
func (mc *MetricsCollector) GetSystemMetrics() *SystemMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metricsCopy := *mc.systemMetrics
	return &metricsCopy
}

// UpdateHealthCheck updates the health status of a component
func (mc *MetricsCollector) UpdateHealthCheck(component, status, message string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	health, exists := mc.healthChecks[component]
	if !exists {
		health = &HealthStatus{
			Component: component,
		}
		mc.healthChecks[component] = health
	}

	health.Status = status
	health.Message = message
	health.LastChecked = time.Now()
	health.CheckCount++

	mc.logger.Debugf("Health check updated for %s: %s - %s", component, status, message)
}

// GetHealthChecks returns all health check results
func (mc *MetricsCollector) GetHealthChecks() map[string]*HealthStatus {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]*HealthStatus)
	for component, health := range mc.healthChecks {
		healthCopy := *health
		result[component] = &healthCopy
	}
	return result
}

// GetMetricsSummary returns a comprehensive summary of all metrics
func (mc *MetricsCollector) GetMetricsSummary() *MetricsSummary {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	summary := &MetricsSummary{
		SystemMetrics: mc.GetSystemMetrics(),
		HealthChecks:  mc.GetHealthChecks(),
		TaskCount:     len(mc.taskExecutions),
	}

	// Calculate aggregate statistics
	var totalExecutions int64
	var totalSuccessful int64
	var totalDuration time.Duration
	var recentExecutions []ExecutionRecord
	var topExecutors []*ExecutionMetrics

	activeTasks := 0

	for _, metrics := range mc.taskExecutions {
		totalExecutions += metrics.TotalExecutions
		totalSuccessful += metrics.SuccessfulExecutions
		totalDuration += metrics.AverageExecutionTime * time.Duration(metrics.TotalExecutions)

		// Check if task has run recently (within last hour)
		if time.Since(metrics.LastExecuted) < time.Hour {
			activeTasks++
		}

		// Collect recent executions (last 20 from all tasks)
		for _, record := range metrics.ExecutionHistory {
			if time.Since(record.Timestamp) < 24*time.Hour {
				recentExecutions = append(recentExecutions, record)
			}
		}

		// Add to top executors (tasks with most executions)
		topExecutors = append(topExecutors, &ExecutionMetrics{
			TaskID:          metrics.TaskID,
			TaskName:        metrics.TaskName,
			TaskType:        metrics.TaskType,
			TotalExecutions: metrics.TotalExecutions,
		})
	}

	summary.TotalExecutions = totalExecutions
	summary.ActiveTasks = activeTasks

	// Calculate success rate
	if totalExecutions > 0 {
		summary.SuccessRate = float64(totalSuccessful) / float64(totalExecutions) * 100
	}

	// Calculate average execution time
	if totalExecutions > 0 {
		summary.AverageExecTime = totalDuration / time.Duration(totalExecutions)
	}

	// Sort and limit recent executions
	if len(recentExecutions) > 20 {
		// Sort by timestamp (most recent first)
		for i := 0; i < len(recentExecutions)-1; i++ {
			for j := i + 1; j < len(recentExecutions); j++ {
				if recentExecutions[i].Timestamp.Before(recentExecutions[j].Timestamp) {
					recentExecutions[i], recentExecutions[j] = recentExecutions[j], recentExecutions[i]
				}
			}
		}
		recentExecutions = recentExecutions[:20]
	}
	summary.RecentExecutions = recentExecutions

	// Sort top executors by execution count
	for i := 0; i < len(topExecutors)-1; i++ {
		for j := i + 1; j < len(topExecutors); j++ {
			if topExecutors[i].TotalExecutions < topExecutors[j].TotalExecutions {
				topExecutors[i], topExecutors[j] = topExecutors[j], topExecutors[i]
			}
		}
	}
	if len(topExecutors) > 10 {
		topExecutors = topExecutors[:10]
	}
	summary.TopExecutors = topExecutors

	return summary
}

// StartPeriodicUpdates starts periodic system metrics updates
func (mc *MetricsCollector) StartPeriodicUpdates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				mc.UpdateSystemMetrics()
			case <-ctx.Done():
				return
			}
		}
	}()

	mc.logger.Infof("Started periodic metrics updates with interval %v", interval)
}

// ExportMetrics exports metrics in JSON format
func (mc *MetricsCollector) ExportMetrics() (string, error) {
	summary := mc.GetMetricsSummary()
	data, err := json.MarshalIndent(summary, "", "  ")
	return string(data), err
}

// PerformHealthChecks runs all health checks
func (mc *MetricsCollector) PerformHealthChecks(schedulerHealthy bool, storageHealthy bool) {
	// Scheduler health check
	if schedulerHealthy {
		mc.UpdateHealthCheck("scheduler", "healthy", "Scheduler is running normally")
	} else {
		mc.UpdateHealthCheck("scheduler", "unhealthy", "Scheduler is not responding")
	}

	// Storage health check
	if storageHealthy {
		mc.UpdateHealthCheck("storage", "healthy", "Storage is accessible")
	} else {
		mc.UpdateHealthCheck("storage", "unhealthy", "Storage is not accessible")
	}

	// System health check based on metrics
	sysMetrics := mc.GetSystemMetrics()
	if sysMetrics.MemoryUsagePercent > 90 {
		mc.UpdateHealthCheck("system", "degraded", fmt.Sprintf("High memory usage: %.1f%%", sysMetrics.MemoryUsagePercent))
	} else if sysMetrics.CPUUsagePercent > 80 {
		mc.UpdateHealthCheck("system", "degraded", fmt.Sprintf("High CPU usage: %.1f%%", sysMetrics.CPUUsagePercent))
	} else {
		mc.UpdateHealthCheck("system", "healthy", "System resources are within normal limits")
	}
}
