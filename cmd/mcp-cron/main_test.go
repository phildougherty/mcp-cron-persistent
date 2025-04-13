package main

import (
	"testing"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/executor"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
)

// TestMCPServerCreation tests server creation with custom configs
func TestMCPServerCreation(t *testing.T) {
	// Test creating MCP server with custom config

	// Create a scheduler and commandExecutor first
	cronScheduler := scheduler.NewScheduler()
	commandExecutor := executor.NewCommandExecutor()

	// Import the config package from the same repo
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9999,
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
	}

	// Create the server with custom config
	mcpServer, err := server.NewMCPServer(cfg, cronScheduler, commandExecutor)

	if err != nil {
		t.Fatalf("Failed to create MCP server: %v", err)
	}

	if mcpServer == nil {
		t.Fatal("NewMCPServer returned nil server")
	}
}
