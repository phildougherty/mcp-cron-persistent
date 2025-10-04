// SPDX-License-Identifier: AGPL-3.0-only
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-cron-persistent/internal/model"

	_ "modernc.org/sqlite"
)

// SQLiteStorage implements task persistence using SQLite
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &SQLiteStorage{db: db}
	if err := storage.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return storage, nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// createTables creates the necessary tables if they don't exist
func (s *SQLiteStorage) createTables() error {
	// Get current schema version
	currentVersion, err := s.getSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Run migrations based on current version
	if err := s.runMigrationsFromVersion(currentVersion); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// runMigrationsFromVersion runs all migrations from the current version to the latest
func (s *SQLiteStorage) runMigrationsFromVersion(currentVersion int) error {
	migrations := []struct {
		version int
		name    string
		sql     string
	}{
		{
			version: 1,
			name:    "initial_schema",
			sql: `
                CREATE TABLE IF NOT EXISTS tasks (
                    id TEXT PRIMARY KEY,
                    name TEXT NOT NULL,
                    description TEXT,
                    command TEXT,
                    prompt TEXT,
                    schedule TEXT NOT NULL,
                    enabled BOOLEAN NOT NULL,
                    type TEXT NOT NULL,
                    last_run TEXT,
                    next_run TEXT,
                    status TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    conversation_id TEXT,
                    conversation_name TEXT,
                    conversation_context TEXT,
                    is_agent BOOLEAN DEFAULT FALSE,
                    agent_personality TEXT,
                    memory_summary TEXT,
                    last_memory_update TEXT
                );

                CREATE TABLE IF NOT EXISTS conversations (
                    id TEXT PRIMARY KEY,
                    name TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    last_used TEXT,
                    context TEXT,
                    type TEXT DEFAULT 'task',
                    description TEXT
                );

                CREATE TABLE IF NOT EXISTS task_memory (
                    task_id TEXT NOT NULL,
                    memory_key TEXT NOT NULL,
                    memory_value TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    PRIMARY KEY (task_id, memory_key),
                    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
                );

                CREATE INDEX IF NOT EXISTS idx_tasks_enabled ON tasks(enabled);
                CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
                CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
                CREATE INDEX IF NOT EXISTS idx_tasks_conversation_id ON tasks(conversation_id);
                CREATE INDEX IF NOT EXISTS idx_tasks_is_agent ON tasks(is_agent);
                CREATE INDEX IF NOT EXISTS idx_conversations_type ON conversations(type);
                CREATE INDEX IF NOT EXISTS idx_conversations_last_used ON conversations(last_used);
                CREATE INDEX IF NOT EXISTS idx_task_memory_task_id ON task_memory(task_id);
            `,
		},
		{
			version: 2,
			name:    "add_dependency_and_watcher_support",
			sql: `
                ALTER TABLE tasks ADD COLUMN depends_on TEXT;
                ALTER TABLE tasks ADD COLUMN trigger_type TEXT;
                ALTER TABLE tasks ADD COLUMN watcher_config TEXT;
                ALTER TABLE tasks ADD COLUMN run_on_demand_only BOOLEAN DEFAULT FALSE;

                CREATE INDEX IF NOT EXISTS idx_tasks_trigger_type ON tasks(trigger_type);
                CREATE INDEX IF NOT EXISTS idx_tasks_run_on_demand_only ON tasks(run_on_demand_only);
            `,
		},
	}

	// Run migrations
	for _, migration := range migrations {
		if currentVersion < migration.version {
			if err := s.executeMigration(migration); err != nil {
				return fmt.Errorf("failed to execute migration %d (%s): %w", migration.version, migration.name, err)
			}

			// Update schema version
			if err := s.setSchemaVersion(migration.version); err != nil {
				return fmt.Errorf("failed to update schema version to %d: %w", migration.version, err)
			}
		}
	}

	return nil
}

// executeMigration executes a single migration
func (s *SQLiteStorage) executeMigration(migration struct {
	version int
	name    string
	sql     string
}) error {
	// Split the SQL into individual statements and execute them
	statements := strings.Split(migration.sql, ";")

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		if _, err := s.db.Exec(stmt); err != nil {
			// For ALTER TABLE statements, check if the column already exists
			if strings.Contains(err.Error(), "duplicate column name") ||
				strings.Contains(err.Error(), "column already exists") {
				continue // Skip if column already exists
			}
			return fmt.Errorf("failed to execute statement: %s, error: %w", stmt, err)
		}
	}

	return nil
}

// getSchemaVersion gets the current schema version
func (s *SQLiteStorage) getSchemaVersion() (int, error) {
	// Create schema_version table if it doesn't exist
	createVersionTable := `
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY
        );
    `
	if _, err := s.db.Exec(createVersionTable); err != nil {
		return 0, err
	}

	// Get current version
	var version int
	err := s.db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	// If no version found, we're starting fresh
	if err == sql.ErrNoRows {
		return 0, nil
	}

	return version, nil
}

// setSchemaVersion sets the current schema version
func (s *SQLiteStorage) setSchemaVersion(version int) error {
	_, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
	return err
}

// SaveTask saves a task to the database
func (s *SQLiteStorage) SaveTask(task *model.Task) error {
	query := `
        INSERT OR REPLACE INTO tasks (
            id, name, description, command, prompt, schedule, enabled, type,
            last_run, next_run, status, created_at, updated_at,
            conversation_id, conversation_name, conversation_context,
            is_agent, agent_personality, memory_summary, last_memory_update,
            depends_on, trigger_type, watcher_config, run_on_demand_only
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `

	// Convert slice and struct to JSON for storage
	dependsOnJSON, _ := json.Marshal(task.DependsOn)
	watcherConfigJSON, _ := json.Marshal(task.WatcherConfig)

	_, err := s.db.Exec(query,
		task.ID,
		task.Name,
		task.Description,
		task.Command,
		task.Prompt,
		task.Schedule,
		task.Enabled,
		task.Type,
		task.LastRun.Format(time.RFC3339),
		task.NextRun.Format(time.RFC3339),
		task.Status,
		task.CreatedAt.Format(time.RFC3339),
		task.UpdatedAt.Format(time.RFC3339),
		task.ConversationID,
		task.ConversationName,
		task.ConversationContext,
		task.IsAgent,
		task.AgentPersonality,
		task.MemorySummary,
		formatTimePtr(task.LastMemoryUpdate),
		string(dependsOnJSON),
		task.TriggerType,
		string(watcherConfigJSON),
		task.RunOnDemandOnly,
	)
	return err
}

// LoadTask loads a task by ID from the database
func (s *SQLiteStorage) LoadTask(id string) (*model.Task, error) {
	// Try new schema first, fall back to old if needed
	query := `
        SELECT id, name, description, command, prompt, schedule, enabled, type,
               last_run, next_run, status, created_at, updated_at,
               conversation_id, conversation_name, conversation_context,
               is_agent, agent_personality, memory_summary, last_memory_update,
               COALESCE(depends_on, '') as depends_on,
               COALESCE(trigger_type, '') as trigger_type,
               COALESCE(watcher_config, '') as watcher_config,
               COALESCE(run_on_demand_only, FALSE) as run_on_demand_only
        FROM tasks WHERE id = ?
    `
	row := s.db.QueryRow(query, id)
	task, err := s.scanTask(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	return task, err
}

// LoadAllTasks loads all tasks from the database
func (s *SQLiteStorage) LoadAllTasks() ([]*model.Task, error) {
	query := `
        SELECT id, name, description, command, prompt, schedule, enabled, type,
               last_run, next_run, status, created_at, updated_at,
               conversation_id, conversation_name, conversation_context,
               is_agent, agent_personality, memory_summary, last_memory_update,
               COALESCE(depends_on, '') as depends_on,
               COALESCE(trigger_type, '') as trigger_type,
               COALESCE(watcher_config, '') as watcher_config,
               COALESCE(run_on_demand_only, FALSE) as run_on_demand_only
        FROM tasks ORDER BY created_at
    `
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		task, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// DeleteTask deletes a task from the database
func (s *SQLiteStorage) DeleteTask(id string) error {
	query := `DELETE FROM tasks WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// scanTask is a helper function to scan a row into a Task struct
func (s *SQLiteStorage) scanTask(scanner interface{ Scan(...interface{}) error }) (*model.Task, error) {
	var task model.Task
	var lastRunStr, nextRunStr, createdAtStr, updatedAtStr string
	var conversationID, conversationName, conversationContext sql.NullString
	var isAgent, runOnDemandOnly sql.NullBool
	var agentPersonality, memorySummary, lastMemoryUpdateStr sql.NullString
	var dependsOnStr, triggerType, watcherConfigStr sql.NullString

	err := scanner.Scan(
		&task.ID,
		&task.Name,
		&task.Description,
		&task.Command,
		&task.Prompt,
		&task.Schedule,
		&task.Enabled,
		&task.Type,
		&lastRunStr,
		&nextRunStr,
		&task.Status,
		&createdAtStr,
		&updatedAtStr,
		&conversationID,
		&conversationName,
		&conversationContext,
		&isAgent,
		&agentPersonality,
		&memorySummary,
		&lastMemoryUpdateStr,
		&dependsOnStr,
		&triggerType,
		&watcherConfigStr,
		&runOnDemandOnly,
	)
	if err != nil {
		return nil, err
	}

	// Parse timestamps
	if task.LastRun, err = time.Parse(time.RFC3339, lastRunStr); err != nil {
		return nil, fmt.Errorf("failed to parse last_run: %w", err)
	}
	if task.NextRun, err = time.Parse(time.RFC3339, nextRunStr); err != nil {
		return nil, fmt.Errorf("failed to parse next_run: %w", err)
	}
	if task.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr); err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}
	if task.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr); err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	// Handle nullable fields
	if conversationID.Valid {
		task.ConversationID = conversationID.String
	}
	if conversationName.Valid {
		task.ConversationName = conversationName.String
	}
	if conversationContext.Valid {
		task.ConversationContext = conversationContext.String
	}
	if isAgent.Valid {
		task.IsAgent = isAgent.Bool
	}
	if agentPersonality.Valid {
		task.AgentPersonality = agentPersonality.String
	}
	if memorySummary.Valid {
		task.MemorySummary = memorySummary.String
	}
	if lastMemoryUpdateStr.Valid && lastMemoryUpdateStr.String != "" {
		if parsed, err := time.Parse(time.RFC3339, lastMemoryUpdateStr.String); err == nil {
			task.LastMemoryUpdate = &parsed
		}
	}

	// Parse new fields with defaults
	if dependsOnStr.Valid && dependsOnStr.String != "" && dependsOnStr.String != "null" {
		if err := json.Unmarshal([]byte(dependsOnStr.String), &task.DependsOn); err != nil {
			// If unmarshaling fails, initialize empty slice
			task.DependsOn = make([]string, 0)
		}
	} else {
		task.DependsOn = make([]string, 0)
	}

	if triggerType.Valid && triggerType.String != "" {
		task.TriggerType = triggerType.String
	} else {
		// Set default trigger type based on task properties
		if len(task.DependsOn) > 0 {
			task.TriggerType = model.TriggerTypeDependency
		} else if task.Schedule != "" {
			task.TriggerType = model.TriggerTypeSchedule
		} else {
			task.TriggerType = model.TriggerTypeManual
		}
	}

	if watcherConfigStr.Valid && watcherConfigStr.String != "" && watcherConfigStr.String != "null" {
		var config model.WatcherConfig
		if err := json.Unmarshal([]byte(watcherConfigStr.String), &config); err == nil {
			task.WatcherConfig = &config
		}
	}

	if runOnDemandOnly.Valid {
		task.RunOnDemandOnly = runOnDemandOnly.Bool
	}

	return &task, nil
}

// SaveTaskResult saves a task execution result
func (s *SQLiteStorage) SaveTaskResult(result *model.Result) error {
	// Create table for results if it doesn't exist
	createTableQuery := `
        CREATE TABLE IF NOT EXISTS task_results (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            command TEXT,
            prompt TEXT,
            output TEXT,
            error TEXT,
            exit_code INTEGER,
            start_time TEXT NOT NULL,
            end_time TEXT NOT NULL,
            duration TEXT NOT NULL,
            created_at TEXT NOT NULL,
            conversation_id TEXT,
            FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
        );
        CREATE INDEX IF NOT EXISTS idx_results_task_id ON task_results(task_id);
        CREATE INDEX IF NOT EXISTS idx_results_start_time ON task_results(start_time);
        CREATE INDEX IF NOT EXISTS idx_results_conversation_id ON task_results(conversation_id);
    `
	if _, err := s.db.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create task_results table: %w", err)
	}

	// Insert the result
	insertQuery := `
        INSERT INTO task_results (
            task_id, command, prompt, output, error, exit_code,
            start_time, end_time, duration, created_at, conversation_id
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `
	_, err := s.db.Exec(insertQuery,
		result.TaskID,
		result.Command,
		result.Prompt,
		result.Output,
		result.Error,
		result.ExitCode,
		result.StartTime.Format(time.RFC3339),
		result.EndTime.Format(time.RFC3339),
		result.Duration,
		time.Now().Format(time.RFC3339),
		result.ConversationID,
	)
	return err
}

// LoadTaskResults loads execution results for a task
func (s *SQLiteStorage) LoadTaskResults(taskID string, limit int) ([]*model.Result, error) {
	query := `
        SELECT task_id, command, prompt, output, error, exit_code,
               start_time, end_time, duration, conversation_id
        FROM task_results
        WHERE task_id = ?
        ORDER BY start_time DESC
        LIMIT ?
    `
	rows, err := s.db.Query(query, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*model.Result
	for rows.Next() {
		result := &model.Result{}
		var startTimeStr, endTimeStr string
		var conversationID sql.NullString
		err := rows.Scan(
			&result.TaskID,
			&result.Command,
			&result.Prompt,
			&result.Output,
			&result.Error,
			&result.ExitCode,
			&startTimeStr,
			&endTimeStr,
			&result.Duration,
			&conversationID,
		)
		if err != nil {
			return nil, err
		}

		if result.StartTime, err = time.Parse(time.RFC3339, startTimeStr); err != nil {
			return nil, fmt.Errorf("failed to parse start_time: %w", err)
		}
		if result.EndTime, err = time.Parse(time.RFC3339, endTimeStr); err != nil {
			return nil, fmt.Errorf("failed to parse end_time: %w", err)
		}

		if conversationID.Valid {
			result.ConversationID = conversationID.String
		}

		results = append(results, result)
	}
	return results, rows.Err()
}

// Conversation management methods
func (s *SQLiteStorage) SaveConversation(conversation *model.Conversation) error {
	query := `
        INSERT OR REPLACE INTO conversations (
            id, name, created_at, updated_at, last_used, context, type, description
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `
	_, err := s.db.Exec(query,
		conversation.ID,
		conversation.Name,
		conversation.CreatedAt.Format(time.RFC3339),
		conversation.UpdatedAt.Format(time.RFC3339),
		conversation.LastUsed.Format(time.RFC3339),
		conversation.Context,
		conversation.Type,
		conversation.Description,
	)
	return err
}

func (s *SQLiteStorage) LoadConversation(id string) (*model.Conversation, error) {
	query := `
        SELECT id, name, created_at, updated_at, last_used, context, type, description
        FROM conversations WHERE id = ?
    `
	row := s.db.QueryRow(query, id)
	var conv model.Conversation
	var createdAtStr, updatedAtStr, lastUsedStr string
	err := row.Scan(
		&conv.ID,
		&conv.Name,
		&createdAtStr,
		&updatedAtStr,
		&lastUsedStr,
		&conv.Context,
		&conv.Type,
		&conv.Description,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("conversation not found")
	}
	if err != nil {
		return nil, err
	}

	if conv.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr); err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}
	if conv.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr); err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}
	if conv.LastUsed, err = time.Parse(time.RFC3339, lastUsedStr); err != nil {
		return nil, fmt.Errorf("failed to parse last_used: %w", err)
	}

	return &conv, nil
}

// Task memory management
func (s *SQLiteStorage) SaveTaskMemory(taskID, key, value string) error {
	query := `
        INSERT OR REPLACE INTO task_memory (
            task_id, memory_key, memory_value, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?)
    `
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(query, taskID, key, value, now, now)
	return err
}

func (s *SQLiteStorage) LoadTaskMemory(taskID string) (map[string]string, error) {
	query := `
        SELECT memory_key, memory_value
        FROM task_memory
        WHERE task_id = ?
        ORDER BY updated_at DESC
    `
	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memory := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		memory[key] = value
	}
	return memory, rows.Err()
}

// RecordTaskRun is a no-op for SQLite (not implemented)
func (s *SQLiteStorage) RecordTaskRun(ctx context.Context, runID, taskID string, startTime, endTime time.Time, output, errorMsg string, exitCode int, status, trigger string) error {
	return nil
}

// Helper functions
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
