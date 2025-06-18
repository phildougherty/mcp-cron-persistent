// SPDX-License-Identifier: AGPL-3.0-only
package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jolks/mcp-cron/internal/model"
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
	query := `
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
            updated_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_tasks_enabled ON tasks(enabled);
        CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
        CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
    `

	_, err := s.db.Exec(query)
	return err
}

// SaveTask saves a task to the database
func (s *SQLiteStorage) SaveTask(task *model.Task) error {
	query := `
        INSERT OR REPLACE INTO tasks (
            id, name, description, command, prompt, schedule, enabled, type,
            last_run, next_run, status, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `

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
	)

	return err
}

// LoadTask loads a task by ID from the database
func (s *SQLiteStorage) LoadTask(id string) (*model.Task, error) {
	query := `
        SELECT id, name, description, command, prompt, schedule, enabled, type,
               last_run, next_run, status, created_at, updated_at
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
               last_run, next_run, status, created_at, updated_at
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
            FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
        );
        CREATE INDEX IF NOT EXISTS idx_results_task_id ON task_results(task_id);
        CREATE INDEX IF NOT EXISTS idx_results_start_time ON task_results(start_time);
    `

	if _, err := s.db.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create task_results table: %w", err)
	}

	// Insert the result
	insertQuery := `
        INSERT INTO task_results (
            task_id, command, prompt, output, error, exit_code,
            start_time, end_time, duration, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
	)

	return err
}

// LoadTaskResults loads execution results for a task
func (s *SQLiteStorage) LoadTaskResults(taskID string, limit int) ([]*model.Result, error) {
	query := `
        SELECT task_id, command, prompt, output, error, exit_code,
               start_time, end_time, duration
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

		results = append(results, result)
	}

	return results, rows.Err()
}
