// SPDX-License-Identifier: AGPL-3.0-only
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
		mcpServersJSON = task.MCPServers
	}

	userID := task.UserID
	if userID == "" {
		userID = "default"
	}

	createdBy := task.CreatedBy
	if createdBy == "" {
		createdBy = "system"
	}

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
			last_memory_update = $17, last_run = $18, next_run = $19
		WHERE id = $1`

	var lastRun, nextRun interface{}
	if !task.LastRun.IsZero() {
		lastRun = task.LastRun
	}
	if !task.NextRun.IsZero() {
		nextRun = task.NextRun
	}

	_, err := s.db.ExecContext(ctx, query,
		task.ID, task.Name, task.Description, task.Type, task.Enabled,
		task.Command, task.Prompt, task.Schedule, task.Status, task.UpdatedAt,
		task.ConversationID, task.ConversationName, task.ConversationContext,
		task.IsAgent, task.AgentPersonality, task.MemorySummary,
		task.LastMemoryUpdate, lastRun, nextRun,
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
		var servers []string
		if err := json.Unmarshal(mcpServersJSON, &servers); err == nil {
			task.MCPServers = servers
		}
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
			   conversation_id, conversation_name, is_agent, chat_session_id
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

		err := rows.Scan(
			&task.ID, &task.Name, &task.Description, &task.Type,
			&task.Enabled, &command, &prompt, &task.Schedule,
			&task.Status, &lastRun, &nextRun, &task.CreatedAt, &task.UpdatedAt,
			&conversationID, &conversationName, &task.IsAgent, &chatSessionID,
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
	return s.RecordTaskRun(
		result.TaskID,
		result.Output,
		result.Error,
		result.ExitCode,
		result.StartTime,
		result.EndTime,
	)
}

// RecordTaskRun saves a task execution result
func (s *PostgresStorage) RecordTaskRun(taskID, output, errorMsg string, exitCode int, startTime, endTime time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := "completed"
	if exitCode != 0 {
		status = "failed"
	}

	query := `
		INSERT INTO task_scheduler.scheduler_task_runs (
			id, task_id, started_at, finished_at, output, error,
			exit_code, status, triggered_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	runID := fmt.Sprintf("run_%s_%d", taskID, time.Now().Unix())

	_, err := s.db.ExecContext(ctx, query,
		runID, taskID, startTime, endTime, output, errorMsg,
		exitCode, status, "scheduler",
	)

	if err != nil {
		return fmt.Errorf("failed to record task run: %w", err)
	}

	return nil
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
