// SPDX-License-Identifier: AGPL-3.0-only
package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"mcp-cron-persistent/internal/model"

	_ "github.com/lib/pq"
)

// PostgresStorage implements task persistence using PostgreSQL
type PostgresStorage struct {
	db *sql.DB
}

// NewPostgresStorage creates a new PostgreSQL storage instance
func NewPostgresStorage(connectionURL string) (*PostgresStorage, error) {
	db, err := sql.Open("postgres", connectionURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Configure connection pool for task scheduler workload
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	// Set default search path to include task_scheduler schema
	_, err = db.ExecContext(ctx, "SET search_path TO task_scheduler, public")
	if err != nil {
		return nil, fmt.Errorf("failed to set search path: %w", err)
	}

	return &PostgresStorage{db: db}, nil
}

// Close closes the database connection
func (s *PostgresStorage) Close() error {
	return s.db.Close()
}

// SaveTask saves or updates a task
func (s *PostgresStorage) SaveTask(task *model.Task) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check if task exists
	var exists bool
	err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM task_scheduler.scheduler_tasks WHERE id = $1)",
		task.ID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check task existence: %w", err)
	}

	if exists {
		return s.updateTask(ctx, task)
	}

	return s.createTask(ctx, task)
}

// createTask creates a new task
func (s *PostgresStorage) createTask(ctx context.Context, task *model.Task) error {
	query := `
		INSERT INTO task_scheduler.scheduler_tasks (
			id, name, description, type, enabled, command, prompt, schedule,
			timezone, status, created_at, updated_at, conversation_id,
			conversation_name, conversation_context, is_agent, agent_personality,
			memory_summary, last_memory_update, user_id, created_by,
			chat_session_id, output_to_chat, inherit_session_context, provider, model, mcp_servers
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
			$22, $23, $24, $25, $26, $27
		)`

	var mcpServersJSON interface{}
	if len(task.MCPServers) > 0 {
		jsonData, err := json.Marshal(task.MCPServers)
		if err != nil {
			return fmt.Errorf("failed to marshal mcp_servers: %w", err)
		}
		mcpServersJSON = jsonData
	}

	userID := task.UserID
	if userID == "" {
		userID = "default"
	}

	createdBy := task.CreatedBy
	if createdBy == "" {
		createdBy = "system"
	}

	fmt.Printf("[DEBUG] createTask: TaskID=%s, ChatSessionID=%s, OutputToChat=%v, InheritSessionContext=%v\n",
		task.ID, task.ChatSessionID, task.OutputToChat, task.InheritSessionContext)

	_, err := s.db.ExecContext(ctx, query,
		task.ID, task.Name, task.Description, task.Type, task.Enabled,
		task.Command, task.Prompt, task.Schedule, "America/New_York",
		task.Status, task.CreatedAt, task.UpdatedAt, task.ConversationID,
		task.ConversationName, task.ConversationContext, task.IsAgent,
		task.AgentPersonality, task.MemorySummary, task.LastMemoryUpdate,
		userID, createdBy,
		task.ChatSessionID, task.OutputToChat, task.InheritSessionContext,
		task.Provider, task.Model, mcpServersJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

// updateTask updates an existing task
func (s *PostgresStorage) updateTask(ctx context.Context, task *model.Task) error {
	query := `
		UPDATE task_scheduler.scheduler_tasks
		SET name = $2, description = $3, type = $4, enabled = $5, command = $6,
			prompt = $7, schedule = $8, status = $9, updated_at = $10,
			conversation_id = $11, conversation_name = $12, conversation_context = $13,
			is_agent = $14, agent_personality = $15, memory_summary = $16,
			last_memory_update = $17, last_run = $18, next_run = $19, chat_session_id = $20,
			output_to_chat = $21, inherit_session_context = $22, provider = $23, model = $24,
			mcp_servers = $25
		WHERE id = $1`

	var lastRun, nextRun interface{}
	if !task.LastRun.IsZero() {
		lastRun = task.LastRun
	}
	if !task.NextRun.IsZero() {
		nextRun = task.NextRun
	}

	var mcpServersJSON interface{}
	if len(task.MCPServers) > 0 {
		jsonData, err := json.Marshal(task.MCPServers)
		if err != nil {
			return fmt.Errorf("failed to marshal mcp_servers: %w", err)
		}
		mcpServersJSON = jsonData
	}

	fmt.Printf("[DEBUG] updateTask: TaskID=%s, ChatSessionID=%s, OutputToChat=%v, InheritSessionContext=%v\n",
		task.ID, task.ChatSessionID, task.OutputToChat, task.InheritSessionContext)

	_, err := s.db.ExecContext(ctx, query,
		task.ID, task.Name, task.Description, task.Type, task.Enabled,
		task.Command, task.Prompt, task.Schedule, task.Status, task.UpdatedAt,
		task.ConversationID, task.ConversationName, task.ConversationContext,
		task.IsAgent, task.AgentPersonality, task.MemorySummary,
		task.LastMemoryUpdate, lastRun, nextRun,
		task.ChatSessionID, task.OutputToChat, task.InheritSessionContext,
		task.Provider, task.Model, mcpServersJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// LoadTask loads a task by ID (alias for GetTask for scheduler.Storage interface)
func (s *PostgresStorage) LoadTask(taskID string) (*model.Task, error) {
	return s.GetTask(taskID)
}

// GetTask retrieves a task by ID
func (s *PostgresStorage) GetTask(taskID string) (*model.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT id, name, description, type, enabled, command, prompt, schedule,
			   status, last_run, next_run, created_at, updated_at,
			   conversation_id, conversation_name, conversation_context,
			   is_agent, agent_personality, memory_summary, last_memory_update,
			   chat_session_id, output_to_chat, inherit_session_context, provider, model, mcp_servers, user_id, created_by
		FROM task_scheduler.scheduler_tasks
		WHERE id = $1`

	var task model.Task
	var lastRun, nextRun, lastMemoryUpdate sql.NullTime
	var conversationID, conversationName, conversationContext sql.NullString
	var agentPersonality, memorySummary sql.NullString
	var chatSessionID, provider, model, userID, createdBy sql.NullString
	var outputToChat, inheritSessionContext sql.NullBool
	var mcpServersJSON []byte

	err := s.db.QueryRowContext(ctx, query, taskID).Scan(
		&task.ID, &task.Name, &task.Description, &task.Type, &task.Enabled,
		&task.Command, &task.Prompt, &task.Schedule, &task.Status,
		&lastRun, &nextRun, &task.CreatedAt, &task.UpdatedAt,
		&conversationID, &conversationName, &conversationContext,
		&task.IsAgent, &agentPersonality, &memorySummary, &lastMemoryUpdate,
		&chatSessionID, &outputToChat, &inheritSessionContext, &provider, &model, &mcpServersJSON, &userID, &createdBy,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	// Handle nullable fields
	if lastRun.Valid {
		task.LastRun = lastRun.Time
	}
	if nextRun.Valid {
		task.NextRun = nextRun.Time
	}
	if conversationID.Valid {
		task.ConversationID = conversationID.String
	}
	if conversationName.Valid {
		task.ConversationName = conversationName.String
	}
	if conversationContext.Valid {
		task.ConversationContext = conversationContext.String
	}
	if agentPersonality.Valid {
		task.AgentPersonality = agentPersonality.String
	}
	if memorySummary.Valid {
		task.MemorySummary = memorySummary.String
	}
	if lastMemoryUpdate.Valid {
		t := lastMemoryUpdate.Time
		task.LastMemoryUpdate = &t
	}
	if chatSessionID.Valid {
		task.ChatSessionID = chatSessionID.String
	}
	if outputToChat.Valid {
		task.OutputToChat = outputToChat.Bool
	}
	if inheritSessionContext.Valid {
		task.InheritSessionContext = inheritSessionContext.Bool
	}
	if provider.Valid {
		task.Provider = provider.String
	}
	if model.Valid {
		task.Model = model.String
	}
	if userID.Valid {
		task.UserID = userID.String
	}
	if createdBy.Valid {
		task.CreatedBy = createdBy.String
	}
	if len(mcpServersJSON) > 0 {
		fmt.Printf("[DEBUG] GetTask: TaskID=%s, mcpServersJSON=%s, len=%d\n", task.ID, string(mcpServersJSON), len(mcpServersJSON))
		var servers []string
		if err := json.Unmarshal(mcpServersJSON, &servers); err == nil {
			task.MCPServers = servers
			fmt.Printf("[DEBUG] GetTask: TaskID=%s, MCPServers=%v\n", task.ID, task.MCPServers)
		} else {
			fmt.Printf("[DEBUG] GetTask: TaskID=%s, unmarshal error: %v\n", task.ID, err)
		}
	} else {
		fmt.Printf("[DEBUG] GetTask: TaskID=%s, mcpServersJSON is empty or nil\n", task.ID)
	}

	return &task, nil
}

// LoadAllTasks loads all tasks from storage (alias for ListTasks for scheduler.Storage interface)
func (s *PostgresStorage) LoadAllTasks() ([]*model.Task, error) {
	return s.ListTasks()
}

// ListTasks retrieves all tasks
func (s *PostgresStorage) ListTasks() ([]*model.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
		SELECT id, name, description, type, enabled, command, prompt, schedule,
			   status, last_run, next_run, created_at, updated_at,
			   conversation_id, conversation_name, is_agent, chat_session_id,
			   output_to_chat, inherit_session_context, provider, model, mcp_servers
		FROM task_scheduler.scheduler_tasks
		ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		var task model.Task
		var lastRun, nextRun sql.NullTime
		var conversationID, conversationName, chatSessionID sql.NullString
		var command, prompt sql.NullString
		var outputToChat, inheritSessionContext sql.NullBool
		var provider, model sql.NullString
		var mcpServersJSON sql.NullString

		err := rows.Scan(
			&task.ID, &task.Name, &task.Description, &task.Type,
			&task.Enabled, &command, &prompt, &task.Schedule,
			&task.Status, &lastRun, &nextRun, &task.CreatedAt, &task.UpdatedAt,
			&conversationID, &conversationName, &task.IsAgent, &chatSessionID,
			&outputToChat, &inheritSessionContext, &provider, &model, &mcpServersJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

		if command.Valid {
			task.Command = command.String
		}
		if prompt.Valid {
			task.Prompt = prompt.String
		}
		if lastRun.Valid {
			task.LastRun = lastRun.Time
		}
		if nextRun.Valid {
			task.NextRun = nextRun.Time
		}
		if conversationID.Valid {
			task.ConversationID = conversationID.String
		}
		if conversationName.Valid {
			task.ConversationName = conversationName.String
		}
		if chatSessionID.Valid {
			task.ChatSessionID = chatSessionID.String
		}
		if outputToChat.Valid {
			task.OutputToChat = outputToChat.Bool
		}
		if inheritSessionContext.Valid {
			task.InheritSessionContext = inheritSessionContext.Bool
		}
		if provider.Valid {
			task.Provider = provider.String
		}
		if model.Valid {
			task.Model = model.String
		}
		if mcpServersJSON.Valid && mcpServersJSON.String != "" {
			if err := json.Unmarshal([]byte(mcpServersJSON.String), &task.MCPServers); err != nil {
				fmt.Printf("Warning: Failed to unmarshal mcp_servers for task %s: %v\n", task.ID, err)
			}
		}

		tasks = append(tasks, &task)
	}

	return tasks, rows.Err()
}

// DeleteTask removes a task
func (s *PostgresStorage) DeleteTask(taskID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM task_scheduler.scheduler_tasks WHERE id = $1",
		taskID,
	)

	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	return nil
}

// SaveTaskMemory saves task-specific memory
func (s *PostgresStorage) SaveTaskMemory(taskID, key, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO task_scheduler.scheduler_task_memory (task_id, memory_key, memory_value, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (task_id, memory_key)
		DO UPDATE SET memory_value = $3, updated_at = $5`

	now := time.Now()
	_, err := s.db.ExecContext(ctx, query, taskID, key, value, now, now)
	if err != nil {
		return fmt.Errorf("failed to save task memory: %w", err)
	}

	return nil
}

// GetTaskMemory retrieves task-specific memory
func (s *PostgresStorage) GetTaskMemory(taskID, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var value string
	err := s.db.QueryRowContext(ctx,
		"SELECT memory_value FROM task_scheduler.scheduler_task_memory WHERE task_id = $1 AND memory_key = $2",
		taskID, key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("memory key not found")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get task memory: %w", err)
	}

	return value, nil
}

// ListTaskMemory lists all memory keys for a task
func (s *PostgresStorage) ListTaskMemory(taskID string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		"SELECT memory_key, memory_value FROM task_scheduler.scheduler_task_memory WHERE task_id = $1",
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query task memory: %w", err)
	}
	defer rows.Close()

	memory := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}
		memory[key] = value
	}

	return memory, rows.Err()
}

// DeleteTaskMemory deletes a specific memory key
func (s *PostgresStorage) DeleteTaskMemory(taskID, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM task_scheduler.scheduler_task_memory WHERE task_id = $1 AND memory_key = $2",
		taskID, key,
	)

	if err != nil {
		return fmt.Errorf("failed to delete task memory: %w", err)
	}

	return nil
}

// SaveTaskResult saves a task execution result (for scheduler.Storage interface)
func (s *PostgresStorage) SaveTaskResult(result *model.Result) error {
	fmt.Printf("[DEBUG] SaveTaskResult: TaskID=%s, Output=%q, Error=%q, ExitCode=%d\n",
		result.TaskID, result.Output, result.Error, result.ExitCode)

	ctx := context.Background()
	runID := fmt.Sprintf("run_%s_%d", result.TaskID, time.Now().Unix())
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}

	return s.RecordTaskRun(
		ctx,
		runID,
		result.TaskID,
		result.StartTime,
		result.EndTime,
		result.Output,
		result.Error,
		result.ExitCode,
		status,
		"scheduler",
	)
}

// RecordTaskRun saves a task execution result
func (s *PostgresStorage) RecordTaskRun(ctx context.Context, runID, taskID string, startTime, endTime time.Time, output, errorMsg string, exitCode int, status, trigger string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if status == "" {
		status = "completed"
		if exitCode != 0 {
			status = "failed"
		}
	}

	if trigger == "" {
		trigger = "scheduler"
	}

	if runID == "" {
		runID = fmt.Sprintf("run_%s_%d", taskID, time.Now().Unix())
	}

	query := `
		INSERT INTO task_scheduler.scheduler_task_runs (
			id, task_id, started_at, finished_at, output, error,
			exit_code, status, triggered_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := s.db.ExecContext(ctxWithTimeout, query,
		runID, taskID, startTime, endTime, output, errorMsg,
		exitCode, status, trigger,
	)

	if err != nil {
		return fmt.Errorf("failed to record task run: %w", err)
	}

	s.postTaskResultToChat(ctxWithTimeout, taskID, runID, output, errorMsg, status)

	return nil
}

func (s *PostgresStorage) postTaskResultToChat(ctx context.Context, taskID, runID, output, errorMsg, status string) {
	var chatSessionID string
	var outputToChat bool

	query := `SELECT chat_session_id, output_to_chat FROM task_scheduler.scheduler_tasks WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, taskID).Scan(&chatSessionID, &outputToChat)

	if err != nil {
		fmt.Printf("[DEBUG] postTaskResultToChat: Failed to query task settings: %v\n", err)
		return
	}

	fmt.Printf("[DEBUG] postTaskResultToChat: taskID=%s, outputToChat=%v, chatSessionID=%s\n", taskID, outputToChat, chatSessionID)

	if !outputToChat {
		fmt.Printf("[DEBUG] postTaskResultToChat: output_to_chat is false, skipping\n")
		return
	}

	if chatSessionID == "" {
		fmt.Printf("[DEBUG] postTaskResultToChat: chat_session_id is empty, skipping\n")
		return
	}

	content := output
	if status == "failed" && errorMsg != "" {
		content = fmt.Sprintf("Task failed with error:\n%s", errorMsg)
	}

	dashboardURL := os.Getenv("DASHBOARD_INTERNAL_URL")
	if dashboardURL == "" {
		dashboardURL = os.Getenv("MCP_COMPOSE_DASHBOARD_URL")
	}
	if dashboardURL == "" {
		dashboardURL = "http://mcp-compose-dashboard:3111"
	}

	fmt.Printf("[INFO] Posting task output to chat: dashboardURL=%s, sessionID=%s, runID=%s\n", dashboardURL, chatSessionID, runID)

	payload := map[string]interface{}{
		"session_id":       chatSessionID,
		"role":             "assistant",
		"content":          content,
		"is_automated":     true,
		"from_task_run_id": runID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("[ERROR] postTaskResultToChat: Failed to marshal payload: %v\n", err)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("%s/api/internal/task-output", dashboardURL),
		"application/json",
		bytes.NewBuffer(data),
	)

	if err != nil {
		fmt.Printf("[ERROR] postTaskResultToChat: Failed to POST to dashboard: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("[DEBUG] postTaskResultToChat: HTTP response status: %d\n", resp.StatusCode)

	if resp.StatusCode == http.StatusOK {
		updateQuery := `UPDATE task_scheduler.scheduler_task_runs SET posted_to_chat = true WHERE id = $1`
		_, updateErr := s.db.ExecContext(ctx, updateQuery, runID)
		if updateErr != nil {
			fmt.Printf("[ERROR] postTaskResultToChat: Failed to update posted_to_chat flag: %v\n", updateErr)
		} else {
			fmt.Printf("[INFO] Successfully posted task output to chat and marked as posted\n")
		}
	} else {
		fmt.Printf("[ERROR] postTaskResultToChat: Dashboard returned non-OK status: %d\n", resp.StatusCode)
	}
}

// Conversation-related methods for backward compatibility
func (s *PostgresStorage) SaveConversation(conv *model.Conversation) error {
	// Conversations are now chat sessions - this is legacy compatibility
	return nil
}

func (s *PostgresStorage) GetConversation(conversationID string) (*model.Conversation, error) {
	return nil, fmt.Errorf("conversations deprecated - use chat sessions")
}

func (s *PostgresStorage) ListConversations() ([]*model.Conversation, error) {
	return []*model.Conversation{}, nil
}

func (s *PostgresStorage) DeleteConversation(conversationID string) error {
	return nil
}
