package server

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/executor"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

func TestNewMCPServer(t *testing.T) {
	// Create dependencies
	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Test with proper config values
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "localhost",
			Port:          8080,
			TransportMode: "sse",
		},
	}
	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server with default config: %v", err)
	}

	if server == nil {
		t.Fatal("NewMCPServer returned nil server")
	}

	// Check values directly
	if server.address != "localhost" {
		t.Errorf("Expected address localhost, got %s", server.address)
	}

	if server.port != 8080 {
		t.Errorf("Expected port 8080, got %d", server.port)
	}
}

func TestNewMCPServerWithCustomConfig(t *testing.T) {
	// Create dependencies
	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Test with custom config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9090,
			TransportMode: "sse",
		},
	}

	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server with custom config: %v", err)
	}

	if server.address != "127.0.0.1" {
		t.Errorf("Expected address 127.0.0.1, got %s", server.address)
	}

	if server.port != 9090 {
		t.Errorf("Expected port 9090, got %d", server.port)
	}
}

func TestMCPServerStartStop(t *testing.T) {
	// Skip in CI environment as this starts a real server
	if _, exists := os.LookupEnv("CI"); exists {
		t.Skip("Skipping test in CI environment")
	}

	// Create dependencies
	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Create server with stdio transport to avoid network binding
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
	}

	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the server
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait a short time to let server initialize
	time.Sleep(100 * time.Millisecond)

	// Stop the server
	if err := server.Stop(); err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}
}

// TestTaskHandlers is a simple smoke test for the handlers
// Note: For a complete test, you would need to mock the protocol package
// which is beyond the scope of this basic test suite
func TestTaskHandlers(t *testing.T) {
	// This is a simple existence test for the handlers
	// Full testing of handlers would require significant mocking of the MCP protocol

	// Create dependencies
	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Create server with stdio transport to avoid network binding
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
	}

	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify that the server has registered tools by checking reflection
	if server.server == nil {
		t.Fatal("Server MCP server instance is nil")
	}
}

// TestTaskCreationTimeFields verifies that LastRun and NextRun are properly initialized
func TestTaskCreationTimeFields(t *testing.T) {
	// Create a scheduler
	sched := scheduler.NewScheduler()

	// Start the scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exec := executor.NewCommandExecutor()

	// Create server
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio",
		},
	}

	server, err := NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start the scheduler after setting the task executor
	sched.Start(ctx)

	beforeTime := time.Now().Add(-time.Second)

	// Create a task using a mock request
	mockRequest := &protocol.CallToolRequest{
		RawArguments: []byte(`{
			"name": "Test Task",
			"schedule": "* * * * * *",
			"command": "echo test",
			"description": "Test task description",
			"enabled": true
		}`),
	}

	result, err := server.handleAddTask(mockRequest)
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}

	// Verify that a result was returned
	if result == nil {
		t.Fatal("handleAddTask returned nil result")
	}

	// Give the scheduler a moment to update NextRun if needed
	time.Sleep(100 * time.Millisecond)

	// Verify that a task was created
	tasks := sched.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, but got %d", len(tasks))
	}

	task := tasks[0]

	// Verify that LastRun and NextRun are properly initialized
	if task.LastRun.IsZero() {
		t.Error("Expected LastRun to be initialized, but it's zero")
	}

	if task.NextRun.IsZero() {
		t.Error("Expected NextRun to be initialized, but it's zero")
	}

	// Verify that LastRun and NextRun are set to a reasonable time (after beforeTime)
	if task.LastRun.Before(beforeTime) {
		t.Errorf("Expected LastRun to be after %v, but got %v", beforeTime, task.LastRun)
	}

	if task.NextRun.Before(beforeTime) {
		t.Errorf("Expected NextRun to be after %v, but got %v", beforeTime, task.NextRun)
	}
}

// TestLogFilePath verifies that the log file path is set correctly
func TestLogFilePath(t *testing.T) {
	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get executable path: %v", err)
	}

	// Get the directory containing the executable
	expectedDir := filepath.Dir(execPath)

	// Create dependencies
	sched := scheduler.NewScheduler()
	exec := executor.NewCommandExecutor()

	// Create a test logger to capture log messages
	var logBuffer bytes.Buffer
	// Save the original output
	originalOutput := log.Writer()
	// Set our buffer as output
	log.SetOutput(&logBuffer)
	// Restore the original output when test completes
	defer log.SetOutput(originalOutput)

	// Set up a mock for os.OpenFile
	originalOpenFile := osOpenFile
	defer func() { osOpenFile = originalOpenFile }()

	var capturedLogPath string
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		capturedLogPath = name
		// Return a temporary file instead of actually opening the log file
		return os.CreateTemp("", "test-log-*")
	}

	// Define server name for the test
	serverName := "mcp-cron"

	// Create server with stdio transport
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio",
			Name:          serverName,
		},
	}

	_, err = NewMCPServer(cfg, sched, exec)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Expected log file name uses the server name
	expectedLogPath := filepath.Join(expectedDir, serverName+".log")

	// Verify that the log path is set to be in the same directory as the executable
	if capturedLogPath != expectedLogPath {
		t.Errorf("Expected log path %s, got %s", expectedLogPath, capturedLogPath)
	}
}
