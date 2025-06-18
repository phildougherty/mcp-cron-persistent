// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test Server defaults
	if cfg.Server.Address != "localhost" {
		t.Errorf("Expected default server address to be 'localhost', got '%s'", cfg.Server.Address)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default server port to be 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.TransportMode != "sse" {
		t.Errorf("Expected default transport mode to be 'sse', got '%s'", cfg.Server.TransportMode)
	}
	if cfg.Server.Name != "mcp-cron" {
		t.Errorf("Expected default server name to be 'mcp-cron', got '%s'", cfg.Server.Name)
	}
	if cfg.Server.Version != "0.1.0" {
		t.Errorf("Expected default server version to be '0.1.0', got '%s'", cfg.Server.Version)
	}

	// Test Scheduler defaults
	if cfg.Scheduler.DefaultTimeout != 10*time.Minute {
		t.Errorf("Expected default timeout to be 10 minutes, got %s", cfg.Scheduler.DefaultTimeout)
	}

	// Test Logging defaults
	if cfg.Logging.Level != "info" {
		t.Errorf("Expected default logging level to be 'info', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.FilePath != "" {
		t.Errorf("Expected default log file path to be empty, got '%s'", cfg.Logging.FilePath)
	}

	// Test OpenWebUI defaults
	if cfg.OpenWebUI.BaseURL != "http://localhost:3000" {
		t.Errorf("Expected default OpenWebUI base URL to be 'http://localhost:3000', got '%s'", cfg.OpenWebUI.BaseURL)
	}
	if cfg.OpenWebUI.APIKey != "" {
		t.Errorf("Expected default OpenWebUI API key to be empty, got '%s'", cfg.OpenWebUI.APIKey)
	}
	if cfg.OpenWebUI.UserID != "scheduler" {
		t.Errorf("Expected default OpenWebUI user ID to be 'scheduler', got '%s'", cfg.OpenWebUI.UserID)
	}
	if cfg.OpenWebUI.RequestTimeout != 60 {
		t.Errorf("Expected default OpenWebUI request timeout to be 60, got %d", cfg.OpenWebUI.RequestTimeout)
	}
	if !cfg.OpenWebUI.Enabled {
		t.Errorf("Expected default OpenWebUI enabled to be true, got %v", cfg.OpenWebUI.Enabled)
	}

	// Test Database defaults
	expectedDBPath := filepath.Join(os.Getenv("HOME"), ".mcp-cron", "mcp-cron.db")
	if cfg.Database.Path != expectedDBPath {
		t.Errorf("Expected default database path to be '%s', got '%s'", expectedDBPath, cfg.Database.Path)
	}
	if !cfg.Database.Enabled {
		t.Errorf("Expected default database enabled to be true, got %v", cfg.Database.Enabled)
	}
}

func TestValidate(t *testing.T) {
	// Test valid config
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default config should be valid, got error: %v", err)
	}

	// Test invalid port (negative)
	invalidPort := DefaultConfig()
	invalidPort.Server.Port = -1
	if err := invalidPort.Validate(); err == nil {
		t.Error("Expected error for negative port, got nil")
	}

	// Test invalid port (too large)
	invalidLargePort := DefaultConfig()
	invalidLargePort.Server.Port = 70000
	if err := invalidLargePort.Validate(); err == nil {
		t.Error("Expected error for port > 65535, got nil")
	}

	// Test invalid transport mode
	invalidTransport := DefaultConfig()
	invalidTransport.Server.TransportMode = "invalid"
	if err := invalidTransport.Validate(); err == nil {
		t.Error("Expected error for invalid transport mode, got nil")
	}

	// Test invalid default timeout (too short)
	invalidTimeout := DefaultConfig()
	invalidTimeout.Scheduler.DefaultTimeout = time.Millisecond * 500
	if err := invalidTimeout.Validate(); err == nil {
		t.Error("Expected error for timeout < 1 second, got nil")
	}

	// Test invalid log level
	invalidLogLevel := DefaultConfig()
	invalidLogLevel.Logging.Level = "invalid"
	if err := invalidLogLevel.Validate(); err == nil {
		t.Error("Expected error for invalid log level, got nil")
	}

	// Test OpenWebUI validation with enabled but missing URL
	invalidOpenWebUI := DefaultConfig()
	invalidOpenWebUI.OpenWebUI.Enabled = true
	invalidOpenWebUI.OpenWebUI.BaseURL = ""
	if err := invalidOpenWebUI.Validate(); err == nil {
		t.Error("Expected error for enabled OpenWebUI with missing base URL, got nil")
	}

	// Test OpenWebUI validation with invalid timeout
	invalidOpenWebUITimeout := DefaultConfig()
	invalidOpenWebUITimeout.OpenWebUI.RequestTimeout = 0
	if err := invalidOpenWebUITimeout.Validate(); err == nil {
		t.Error("Expected error for OpenWebUI request timeout < 1, got nil")
	}
}

func TestFromEnv(t *testing.T) {
	// Save current environment variables
	originalVars := map[string]string{
		"MCP_CRON_SERVER_ADDRESS":            os.Getenv("MCP_CRON_SERVER_ADDRESS"),
		"MCP_CRON_SERVER_PORT":               os.Getenv("MCP_CRON_SERVER_PORT"),
		"MCP_CRON_SERVER_TRANSPORT":          os.Getenv("MCP_CRON_SERVER_TRANSPORT"),
		"MCP_CRON_SERVER_NAME":               os.Getenv("MCP_CRON_SERVER_NAME"),
		"MCP_CRON_SERVER_VERSION":            os.Getenv("MCP_CRON_SERVER_VERSION"),
		"MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT": os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"),
		"MCP_CRON_LOGGING_LEVEL":             os.Getenv("MCP_CRON_LOGGING_LEVEL"),
		"MCP_CRON_LOGGING_FILE":              os.Getenv("MCP_CRON_LOGGING_FILE"),
		"MCP_CRON_OPENWEBUI_BASE_URL":        os.Getenv("MCP_CRON_OPENWEBUI_BASE_URL"),
		"MCP_CRON_OPENWEBUI_API_KEY":         os.Getenv("MCP_CRON_OPENWEBUI_API_KEY"),
		"MCP_CRON_OPENWEBUI_MODEL":           os.Getenv("MCP_CRON_OPENWEBUI_MODEL"),
		"MCP_CRON_OPENWEBUI_USER_ID":         os.Getenv("MCP_CRON_OPENWEBUI_USER_ID"),
		"MCP_CRON_OPENWEBUI_REQUEST_TIMEOUT": os.Getenv("MCP_CRON_OPENWEBUI_REQUEST_TIMEOUT"),
		"MCP_CRON_OPENWEBUI_ENABLED":         os.Getenv("MCP_CRON_OPENWEBUI_ENABLED"),
		"MCP_CRON_DATABASE_PATH":             os.Getenv("MCP_CRON_DATABASE_PATH"),
		"MCP_CRON_DATABASE_ENABLED":          os.Getenv("MCP_CRON_DATABASE_ENABLED"),
	}

	// Restore environment variables after test
	defer func() {
		for key, value := range originalVars {
			if value != "" {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	// Clear all relevant environment variables
	for key := range originalVars {
		os.Unsetenv(key)
	}

	// Set test values
	os.Setenv("MCP_CRON_SERVER_ADDRESS", "127.0.0.1")
	os.Setenv("MCP_CRON_SERVER_PORT", "9090")
	os.Setenv("MCP_CRON_SERVER_TRANSPORT", "stdio")
	os.Setenv("MCP_CRON_SERVER_NAME", "test-server")
	os.Setenv("MCP_CRON_SERVER_VERSION", "1.0.0")
	os.Setenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT", "5m")
	os.Setenv("MCP_CRON_LOGGING_LEVEL", "debug")
	os.Setenv("MCP_CRON_LOGGING_FILE", "/tmp/test.log")
	os.Setenv("MCP_CRON_OPENWEBUI_BASE_URL", "http://test-openwebui:3000")
	os.Setenv("MCP_CRON_OPENWEBUI_API_KEY", "test-api-key")
	os.Setenv("MCP_CRON_OPENWEBUI_MODEL", "test-model")
	os.Setenv("MCP_CRON_OPENWEBUI_USER_ID", "test-user")
	os.Setenv("MCP_CRON_OPENWEBUI_REQUEST_TIMEOUT", "120")
	os.Setenv("MCP_CRON_OPENWEBUI_ENABLED", "true")
	os.Setenv("MCP_CRON_DATABASE_PATH", "/tmp/test.db")
	os.Setenv("MCP_CRON_DATABASE_ENABLED", "true")

	// Create a new config and apply environment variables
	cfg := DefaultConfig()
	FromEnv(cfg)

	// Verify values were loaded from environment
	if cfg.Server.Address != "127.0.0.1" {
		t.Errorf("Expected server address '127.0.0.1', got '%s'", cfg.Server.Address)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Expected server port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.TransportMode != "stdio" {
		t.Errorf("Expected transport mode 'stdio', got '%s'", cfg.Server.TransportMode)
	}
	if cfg.Server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", cfg.Server.Name)
	}
	if cfg.Server.Version != "1.0.0" {
		t.Errorf("Expected server version '1.0.0', got '%s'", cfg.Server.Version)
	}
	if cfg.Scheduler.DefaultTimeout != 5*time.Minute {
		t.Errorf("Expected default timeout 5m, got %s", cfg.Scheduler.DefaultTimeout)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected logging level 'debug', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.FilePath != "/tmp/test.log" {
		t.Errorf("Expected log file path '/tmp/test.log', got '%s'", cfg.Logging.FilePath)
	}
	if cfg.OpenWebUI.BaseURL != "http://test-openwebui:3000" {
		t.Errorf("Expected OpenWebUI base URL 'http://test-openwebui:3000', got '%s'", cfg.OpenWebUI.BaseURL)
	}
	if cfg.OpenWebUI.APIKey != "test-api-key" {
		t.Errorf("Expected OpenWebUI API key 'test-api-key', got '%s'", cfg.OpenWebUI.APIKey)
	}
	if cfg.OpenWebUI.Model != "test-model" {
		t.Errorf("Expected OpenWebUI model 'test-model', got '%s'", cfg.OpenWebUI.Model)
	}
	if cfg.OpenWebUI.UserID != "test-user" {
		t.Errorf("Expected OpenWebUI user ID 'test-user', got '%s'", cfg.OpenWebUI.UserID)
	}
	if cfg.OpenWebUI.RequestTimeout != 120 {
		t.Errorf("Expected OpenWebUI request timeout 120, got %d", cfg.OpenWebUI.RequestTimeout)
	}
	if !cfg.OpenWebUI.Enabled {
		t.Errorf("Expected OpenWebUI enabled true, got false")
	}
	if cfg.Database.Path != "/tmp/test.db" {
		t.Errorf("Expected database path '/tmp/test.db', got '%s'", cfg.Database.Path)
	}
	if !cfg.Database.Enabled {
		t.Errorf("Expected database enabled true, got false")
	}

	// Test invalid port format
	os.Setenv("MCP_CRON_SERVER_PORT", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected server port to remain 8080 for invalid input, got %d", cfg.Server.Port)
	}

	// Test invalid timeout format
	os.Setenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Scheduler.DefaultTimeout != 10*time.Minute {
		t.Errorf("Expected default timeout to remain 10m for invalid input, got %s", cfg.Scheduler.DefaultTimeout)
	}

	// Test invalid request timeout format
	os.Setenv("MCP_CRON_OPENWEBUI_REQUEST_TIMEOUT", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.OpenWebUI.RequestTimeout != 60 {
		t.Errorf("Expected OpenWebUI request timeout to remain 60 for invalid input, got %d", cfg.OpenWebUI.RequestTimeout)
	}
}
