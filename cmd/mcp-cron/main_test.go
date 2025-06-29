// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"testing"

	"mcp-cron-persistent/internal/agent"
	"mcp-cron-persistent/internal/command"
	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/scheduler"
	"mcp-cron-persistent/internal/server"
)

// TestMCPServerCreation tests server creation with custom configs
func TestMCPServerCreation(t *testing.T) {
	// Test creating MCP server with custom config

	// Import the config package from the same repo
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9999,
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: config.SchedulerConfig{
			DefaultTimeout: config.DefaultConfig().Scheduler.DefaultTimeout,
		},
	}

	// Create a scheduler and executors first
	cronScheduler := scheduler.NewScheduler(&cfg.Scheduler)
	commandExecutor := command.NewCommandExecutor()

	// Create agent executor with config
	agentExecutor := agent.NewAgentExecutor(cfg)

	// Create the server with custom config
	mcpServer, err := server.NewMCPServer(cfg, cronScheduler, commandExecutor, agentExecutor)

	if err != nil {
		t.Fatalf("Failed to create MCP server: %v", err)
	}

	if mcpServer == nil {
		t.Fatal("NewMCPServer returned nil server")
	}
}
