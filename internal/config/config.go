// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	Server ServerConfig

	// Scheduler configuration
	Scheduler SchedulerConfig

	// Logging configuration
	Logging LoggingConfig
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	// Address to bind to
	Address string

	// Port to listen on
	Port int

	// Transport mode (sse, stdio)
	TransportMode string

	// Server name
	Name string

	// Server version
	Version string
}

// SchedulerConfig holds scheduler-specific configuration
type SchedulerConfig struct {
	// Maximum concurrent tasks
	MaxConcurrent int

	// Default task timeout
	DefaultTimeout time.Duration

	// Task execution directory
	ExecutionDir string
}

// LoggingConfig holds logging-specific configuration
type LoggingConfig struct {
	// Log level (debug, info, warn, error, fatal)
	Level string

	// Log file path (optional)
	FilePath string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:       "localhost",
			Port:          8080,
			TransportMode: "sse",
			Name:          "mcp-cron",
			Version:       "0.1.0",
		},
		Scheduler: SchedulerConfig{
			MaxConcurrent:  5,
			DefaultTimeout: 10 * time.Minute,
			ExecutionDir:   "./",
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate server config
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server port must be between 0 and 65535")
	}

	if c.Server.TransportMode != "sse" && c.Server.TransportMode != "stdio" {
		return fmt.Errorf("transport mode must be either 'sse' or 'stdio'")
	}

	// Validate scheduler config
	if c.Scheduler.MaxConcurrent < 1 {
		return fmt.Errorf("max concurrent tasks must be at least 1")
	}

	if c.Scheduler.DefaultTimeout < time.Second {
		return fmt.Errorf("default timeout must be at least 1 second")
	}

	// Validate logging config
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "error", "fatal":
		// Valid log level
	default:
		return fmt.Errorf("log level must be one of: debug, info, warn, error, fatal")
	}

	return nil
}

// FromEnv loads configuration from environment variables
func FromEnv(config *Config) {
	// Server configuration
	if val := os.Getenv("MCP_SERVER_ADDRESS"); val != "" {
		config.Server.Address = val
	}

	if val := os.Getenv("MCP_SERVER_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Server.Port = port
		}
	}

	if val := os.Getenv("MCP_SERVER_TRANSPORT"); val != "" {
		config.Server.TransportMode = val
	}

	if val := os.Getenv("MCP_SERVER_NAME"); val != "" {
		config.Server.Name = val
	}

	if val := os.Getenv("MCP_SERVER_VERSION"); val != "" {
		config.Server.Version = val
	}

	// Scheduler configuration
	if val := os.Getenv("MCP_SCHEDULER_MAX_CONCURRENT"); val != "" {
		if maxConcurrent, err := strconv.Atoi(val); err == nil {
			config.Scheduler.MaxConcurrent = maxConcurrent
		}
	}

	if val := os.Getenv("MCP_SCHEDULER_DEFAULT_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Scheduler.DefaultTimeout = duration
		}
	}

	if val := os.Getenv("MCP_SCHEDULER_EXECUTION_DIR"); val != "" {
		config.Scheduler.ExecutionDir = val
	}

	// Logging configuration
	if val := os.Getenv("MCP_LOGGING_LEVEL"); val != "" {
		config.Logging.Level = val
	}

	if val := os.Getenv("MCP_LOGGING_FILE"); val != "" {
		config.Logging.FilePath = val
	}
}
