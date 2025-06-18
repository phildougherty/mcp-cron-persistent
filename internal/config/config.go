// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	// AI configuration
	AI AIConfig
	// Database configuration
	Database DatabaseConfig
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
	// Default task timeout
	DefaultTimeout time.Duration
}

// LoggingConfig holds logging-specific configuration
type LoggingConfig struct {
	// Log level (debug, info, warn, error, fatal)
	Level string
	// Log file path (optional)
	FilePath string
}

// AIConfig holds AI-specific configuration
type AIConfig struct {
	// Primary LLM provider (ollama, openrouter, openai)
	Provider string

	// Ollama configuration
	OllamaHost  string
	OllamaPort  int
	OllamaModel string

	// OpenRouter configuration
	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenRouterURL    string

	// OpenAI configuration (legacy/fallback)
	OpenAIAPIKey string
	Model        string // Legacy field for backwards compatibility

	// General AI settings
	EnableOpenAITests bool
	MaxToolIterations int
	MCPConfigFilePath string

	// Request timeout (in seconds)
	RequestTimeout int
}

// DatabaseConfig holds database-specific configuration
type DatabaseConfig struct {
	// SQLite database path
	Path string
	// Enable database persistence
	Enabled bool
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
			DefaultTimeout: 10 * time.Minute,
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
		AI: AIConfig{
			Provider:          "ollama", // Default to Ollama
			OllamaHost:        "desk",   // Default to desk
			OllamaPort:        11434,
			OllamaModel:       "qwen3:14b",
			OpenRouterAPIKey:  "",
			OpenRouterModel:   "anthropic/claude-3.5-sonnet",
			OpenRouterURL:     "https://openrouter.ai/api/v1/chat/completions",
			OpenAIAPIKey:      "",
			Model:             "gpt-4o", // Legacy field
			EnableOpenAITests: false,
			MaxToolIterations: 20,
			MCPConfigFilePath: filepath.Join(os.Getenv("HOME"), ".cursor", "mcp.json"),
			RequestTimeout:    30, // 30 seconds
		},
		Database: DatabaseConfig{
			Path:    filepath.Join(os.Getenv("HOME"), ".mcp-cron", "mcp-cron.db"),
			Enabled: true,
		},
	}
}

// GetOllamaEndpoint returns the full Ollama endpoint URL
func (a *AIConfig) GetOllamaEndpoint() string {
	return fmt.Sprintf("http://%s:%d", a.OllamaHost, a.OllamaPort)
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

	// Validate AI config
	if c.AI.MaxToolIterations < 1 {
		return fmt.Errorf("max tool iterations must be at least 1")
	}

	// Validate AI provider
	switch strings.ToLower(c.AI.Provider) {
	case "ollama", "openrouter", "openai":
		// Valid providers
	default:
		return fmt.Errorf("AI provider must be one of: ollama, openrouter, openai")
	}

	// Validate provider-specific configs
	switch strings.ToLower(c.AI.Provider) {
	case "openrouter":
		if c.AI.OpenRouterAPIKey == "" {
			return fmt.Errorf("OpenRouter API key is required when using openrouter provider")
		}
		if c.AI.OpenRouterModel == "" {
			return fmt.Errorf("OpenRouter model is required when using openrouter provider")
		}
	case "openai":
		if c.AI.OpenAIAPIKey == "" {
			return fmt.Errorf("OpenAI API key is required when using openai provider")
		}
	case "ollama":
		if c.AI.OllamaHost == "" {
			return fmt.Errorf("ollama host is required when using ollama provider")
		}
		if c.AI.OllamaPort <= 0 || c.AI.OllamaPort > 65535 {
			return fmt.Errorf("ollama port must be between 1 and 65535")
		}
		if c.AI.OllamaModel == "" {
			return fmt.Errorf("ollama model is required when using ollama provider")
		}
	}

	// Validate database config
	if c.Database.Enabled {
		// Ensure database directory exists
		dbDir := filepath.Dir(c.Database.Path)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory %s: %w", dbDir, err)
		}
	}

	return nil
}

// FromEnv loads configuration from environment variables
func FromEnv(config *Config) {
	// Server configuration
	if val := os.Getenv("MCP_CRON_SERVER_ADDRESS"); val != "" {
		config.Server.Address = val
	}
	if val := os.Getenv("MCP_CRON_SERVER_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Server.Port = port
		}
	}
	if val := os.Getenv("MCP_CRON_SERVER_TRANSPORT"); val != "" {
		config.Server.TransportMode = val
	}
	if val := os.Getenv("MCP_CRON_SERVER_NAME"); val != "" {
		config.Server.Name = val
	}
	if val := os.Getenv("MCP_CRON_SERVER_VERSION"); val != "" {
		config.Server.Version = val
	}

	// Scheduler configuration
	if val := os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Scheduler.DefaultTimeout = duration
		}
	}

	// Logging configuration
	if val := os.Getenv("MCP_CRON_LOGGING_LEVEL"); val != "" {
		config.Logging.Level = val
	}
	if val := os.Getenv("MCP_CRON_LOGGING_FILE"); val != "" {
		config.Logging.FilePath = val
	}

	// AI configuration
	if val := os.Getenv("MCP_CRON_AI_PROVIDER"); val != "" {
		config.AI.Provider = val
	}

	// Ollama configuration
	if val := os.Getenv("MCP_CRON_OLLAMA_HOST"); val != "" {
		config.AI.OllamaHost = val
	}
	if val := os.Getenv("MCP_CRON_OLLAMA_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.AI.OllamaPort = port
		}
	}
	if val := os.Getenv("MCP_CRON_OLLAMA_MODEL"); val != "" {
		config.AI.OllamaModel = val
	}

	// OpenRouter configuration
	if val := os.Getenv("OPENROUTER_API_KEY"); val != "" {
		config.AI.OpenRouterAPIKey = val
	}
	if val := os.Getenv("MCP_CRON_OPENROUTER_API_KEY"); val != "" {
		config.AI.OpenRouterAPIKey = val
	}
	if val := os.Getenv("MCP_CRON_OPENROUTER_MODEL"); val != "" {
		config.AI.OpenRouterModel = val
	}
	if val := os.Getenv("MCP_CRON_OPENROUTER_URL"); val != "" {
		config.AI.OpenRouterURL = val
	}

	// OpenAI configuration (legacy)
	if val := os.Getenv("OPENAI_API_KEY"); val != "" {
		config.AI.OpenAIAPIKey = val
	}
	if val := os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS"); val != "" {
		config.AI.EnableOpenAITests = strings.ToLower(val) == "true"
	}
	if val := os.Getenv("MCP_CRON_AI_MODEL"); val != "" {
		config.AI.Model = val
	}
	if val := os.Getenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS"); val != "" {
		if iterations, err := strconv.Atoi(val); err == nil {
			config.AI.MaxToolIterations = iterations
		}
	}
	if val := os.Getenv("MCP_CRON_MCP_CONFIG_FILE_PATH"); val != "" {
		config.AI.MCPConfigFilePath = val
	}
	if val := os.Getenv("MCP_CRON_AI_REQUEST_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil {
			config.AI.RequestTimeout = timeout
		}
	}

	// Database configuration
	if val := os.Getenv("MCP_CRON_DATABASE_PATH"); val != "" {
		config.Database.Path = val
	}
	if val := os.Getenv("MCP_CRON_DATABASE_ENABLED"); val != "" {
		config.Database.Enabled = strings.ToLower(val) == "true"
	}
}
