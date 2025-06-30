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
	// OpenWebUI integration configuration
	OpenWebUI OpenWebUIConfig
	// Database configuration
	Database    DatabaseConfig
	OpenRouter  OpenRouterConfig
	Ollama      OllamaConfig
	ModelRouter ModelRouterConfig
	// Legacy flag
	UseOpenRouter bool
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

// OpenWebUIConfig holds OpenWebUI integration configuration
type OpenWebUIConfig struct {
	// Base URL for OpenWebUI instance
	BaseURL string
	// API key for authentication
	APIKey string
	// Default model to use for AI tasks
	Model string
	// Default user ID for API calls
	UserID string
	// Request timeout in seconds
	RequestTimeout int
	// Enable OpenWebUI integration
	Enabled bool
}

// DatabaseConfig holds database-specific configuration
type DatabaseConfig struct {
	// SQLite database path
	Path string
	// Enable database persistence
	Enabled bool
}

// OllamaConfig holds Ollama configuration
type OllamaConfig struct {
	BaseURL        string `json:"base_url"`
	DefaultModel   string `json:"default_model"`
	Enabled        bool   `json:"enabled"`
	RequestTimeout int    `json:"request_timeout"`
}

// OpenRouterConfig holds OpenRouter integration configuration
type OpenRouterConfig struct {
	APIKey         string `json:"api_key"`
	DefaultModel   string `json:"default_model"`
	MCPProxyURL    string `json:"mcp_proxy_url"`
	MCPProxyKey    string `json:"mcp_proxy_key"`
	RequestTimeout int    `json:"request_timeout"`
	Enabled        bool   `json:"enabled"`
}

// ModelRouterConfig holds model router configuration
type ModelRouterConfig struct {
	Enabled          bool    `json:"enabled"`
	PreferLocal      bool    `json:"prefer_local"`
	FallbackToCloud  bool    `json:"fallback_to_cloud"`
	DefaultHint      string  `json:"default_hint"` // fast, cheap, powerful, local, balanced
	MaxCostPerTask   float64 `json:"max_cost_per_task"`
	SpeedWeight      float64 `json:"speed_weight"`
	CostWeight       float64 `json:"cost_weight"`
	CapabilityWeight float64 `json:"capability_weight"`
	SecurityWeight   float64 `json:"security_weight"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:       "localhost",
			Port:          8080,
			Name:          "mcp-cron",
			Version:       "0.1.0",
			TransportMode: "sse",
		},
		Scheduler: SchedulerConfig{
			DefaultTimeout: 10 * time.Minute,
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
		OpenWebUI: OpenWebUIConfig{
			BaseURL:        "http://localhost:3000",
			APIKey:         "",
			Model:          "gpt-4o",
			UserID:         "",
			RequestTimeout: 60,
			Enabled:        false,
		},
		Database: DatabaseConfig{
			Path:    "./mcp-cron.db",
			Enabled: true,
		},
		Ollama: OllamaConfig{
			BaseURL:        "http://localhost:11434",
			DefaultModel:   "llama3.2:3b", // Lightweight default
			Enabled:        true,
			RequestTimeout: 60,
		},
		OpenRouter: OpenRouterConfig{
			APIKey:         "",
			DefaultModel:   "anthropic/claude-3.5-sonnet",
			MCPProxyURL:    "http://localhost:3001",
			MCPProxyKey:    "",
			RequestTimeout: 120,
			Enabled:        false,
		},
		ModelRouter: ModelRouterConfig{
			Enabled:          true,
			PreferLocal:      true,
			FallbackToCloud:  true,
			DefaultHint:      "balanced",
			MaxCostPerTask:   0.50,
			SpeedWeight:      0.3,
			CostWeight:       0.4,
			CapabilityWeight: 0.2,
			SecurityWeight:   0.1,
		},
		UseOpenRouter: false,
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

	// Auto-logic: if USE_OPENROUTER is true or OpenRouter is enabled, disable OpenWebUI
	if c.UseOpenRouter || c.OpenRouter.Enabled {
		c.OpenWebUI.Enabled = false
	}

	// Validate OpenRouter config if enabled
	if c.UseOpenRouter || c.OpenRouter.Enabled {
		if c.OpenRouter.APIKey == "" {
			return fmt.Errorf("OpenRouter API key is required when OpenRouter integration is enabled (set OPENROUTER_API_KEY environment variable)")
		}
		if c.OpenRouter.MCPProxyURL == "" {
			return fmt.Errorf("MCP proxy URL is required when OpenRouter integration is enabled (set MCP_PROXY_URL environment variable)")
		}
		if c.OpenRouter.MCPProxyKey == "" {
			return fmt.Errorf("MCP proxy API key is required when OpenRouter integration is enabled (set MCP_PROXY_API_KEY environment variable)")
		}
	}

	// Validate OpenWebUI config if enabled and OpenRouter is not being used
	if c.OpenWebUI.Enabled && !c.UseOpenRouter && !c.OpenRouter.Enabled {
		if c.OpenWebUI.BaseURL == "" {
			return fmt.Errorf("OpenWebUI base URL is required when OpenWebUI integration is enabled")
		}
		if c.OpenWebUI.RequestTimeout < 1 {
			return fmt.Errorf("OpenWebUI request timeout must be at least 1 second")
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

func FromEnv(cfg *Config) {
	if addr := os.Getenv("MCP_CRON_SERVER_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
	}
	if port := os.Getenv("MCP_CRON_SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	}
	if name := os.Getenv("MCP_CRON_SERVER_NAME"); name != "" {
		cfg.Server.Name = name
	}
	if version := os.Getenv("MCP_CRON_SERVER_VERSION"); version != "" {
		cfg.Server.Version = version
	}
	if transport := os.Getenv("MCP_CRON_SERVER_TRANSPORT"); transport != "" {
		cfg.Server.TransportMode = transport
	}

	if timeout := os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			cfg.Scheduler.DefaultTimeout = t
		}
	}

	if level := os.Getenv("MCP_CRON_LOGGING_LEVEL"); level != "" {
		cfg.Logging.Level = level
	}
	if filePath := os.Getenv("MCP_CRON_LOGGING_FILE_PATH"); filePath != "" {
		cfg.Logging.FilePath = filePath
	}

	// OpenWebUI configuration
	if baseURL := os.Getenv("MCP_CRON_OPENWEBUI_BASE_URL"); baseURL != "" {
		cfg.OpenWebUI.BaseURL = baseURL
	}
	if apiKey := os.Getenv("MCP_CRON_OPENWEBUI_API_KEY"); apiKey != "" {
		cfg.OpenWebUI.APIKey = apiKey
	}
	if model := os.Getenv("MCP_CRON_OPENWEBUI_MODEL"); model != "" {
		cfg.OpenWebUI.Model = model
	}
	if userID := os.Getenv("MCP_CRON_OPENWEBUI_USER_ID"); userID != "" {
		cfg.OpenWebUI.UserID = userID
	}
	if timeout := os.Getenv("MCP_CRON_OPENWEBUI_REQUEST_TIMEOUT"); timeout != "" {
		if t, err := strconv.Atoi(timeout); err == nil {
			cfg.OpenWebUI.RequestTimeout = t
		}
	}
	if enabled := os.Getenv("MCP_CRON_OPENWEBUI_ENABLED"); enabled != "" {
		cfg.OpenWebUI.Enabled = enabled == "true"
	}

	// Database configuration
	if dbPath := os.Getenv("MCP_CRON_DATABASE_PATH"); dbPath != "" {
		cfg.Database.Path = dbPath
	}
	if enabled := os.Getenv("MCP_CRON_DATABASE_ENABLED"); enabled != "" {
		cfg.Database.Enabled = enabled == "true"
	}

	// Ollama configuration
	if baseURL := os.Getenv("MCP_CRON_OLLAMA_BASE_URL"); baseURL != "" {
		cfg.Ollama.BaseURL = baseURL
	}
	if model := os.Getenv("MCP_CRON_OLLAMA_DEFAULT_MODEL"); model != "" {
		cfg.Ollama.DefaultModel = model
	}
	if enabled := os.Getenv("MCP_CRON_OLLAMA_ENABLED"); enabled != "" {
		cfg.Ollama.Enabled = enabled == "true"
	}
	if timeout := os.Getenv("MCP_CRON_OLLAMA_REQUEST_TIMEOUT"); timeout != "" {
		if t, err := strconv.Atoi(timeout); err == nil {
			cfg.Ollama.RequestTimeout = t
		}
	}

	// OpenRouter configuration
	if apiKey := os.Getenv("MCP_CRON_OPENROUTER_API_KEY"); apiKey != "" {
		cfg.OpenRouter.APIKey = apiKey
		cfg.OpenRouter.Enabled = true
	}
	if model := os.Getenv("MCP_CRON_OPENROUTER_DEFAULT_MODEL"); model != "" {
		cfg.OpenRouter.DefaultModel = model
	}
	if proxyURL := os.Getenv("MCP_CRON_OPENROUTER_MCP_PROXY_URL"); proxyURL != "" {
		cfg.OpenRouter.MCPProxyURL = proxyURL
	}
	if proxyKey := os.Getenv("MCP_CRON_OPENROUTER_MCP_PROXY_KEY"); proxyKey != "" {
		cfg.OpenRouter.MCPProxyKey = proxyKey
	}
	if enabled := os.Getenv("MCP_CRON_OPENROUTER_ENABLED"); enabled != "" {
		cfg.OpenRouter.Enabled = enabled == "true"
	}
	if timeout := os.Getenv("MCP_CRON_OPENROUTER_REQUEST_TIMEOUT"); timeout != "" {
		if t, err := strconv.Atoi(timeout); err == nil {
			cfg.OpenRouter.RequestTimeout = t
		}
	}

	// Model Router configuration
	if enabled := os.Getenv("MCP_CRON_MODEL_ROUTER_ENABLED"); enabled != "" {
		cfg.ModelRouter.Enabled = enabled == "true"
	}
	if preferLocal := os.Getenv("MCP_CRON_MODEL_ROUTER_PREFER_LOCAL"); preferLocal != "" {
		cfg.ModelRouter.PreferLocal = preferLocal == "true"
	}
	if fallback := os.Getenv("MCP_CRON_MODEL_ROUTER_FALLBACK_TO_CLOUD"); fallback != "" {
		cfg.ModelRouter.FallbackToCloud = fallback == "true"
	}
	if hint := os.Getenv("MCP_CRON_MODEL_ROUTER_DEFAULT_HINT"); hint != "" {
		cfg.ModelRouter.DefaultHint = hint
	}
	if maxCost := os.Getenv("MCP_CRON_MODEL_ROUTER_MAX_COST_PER_TASK"); maxCost != "" {
		if cost, err := strconv.ParseFloat(maxCost, 64); err == nil {
			cfg.ModelRouter.MaxCostPerTask = cost
		}
	}

	// Legacy OpenRouter support
	if os.Getenv("OPENROUTER_API_KEY") != "" && cfg.OpenRouter.APIKey == "" {
		cfg.OpenRouter.APIKey = os.Getenv("OPENROUTER_API_KEY")
		cfg.OpenRouter.Enabled = true
	}
	if useOpenRouter := os.Getenv("MCP_CRON_USE_OPENROUTER"); useOpenRouter != "" {
		cfg.UseOpenRouter = useOpenRouter == "true"
	}

	// Legacy MCP proxy environment variables
	if mcpProxyURL := os.Getenv("MCP_PROXY_URL"); mcpProxyURL != "" && cfg.OpenRouter.MCPProxyURL == "" {
		cfg.OpenRouter.MCPProxyURL = mcpProxyURL
	}
	if mcpProxyKey := os.Getenv("MCP_PROXY_API_KEY"); mcpProxyKey != "" && cfg.OpenRouter.MCPProxyKey == "" {
		cfg.OpenRouter.MCPProxyKey = mcpProxyKey
	}
}
