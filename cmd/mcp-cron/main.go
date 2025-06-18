// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
	"github.com/jolks/mcp-cron/internal/storage"
)

var (
	address         = flag.String("address", "", "The address to bind the server to")
	port            = flag.Int("port", 0, "The port to bind the server to")
	transport       = flag.String("transport", "", "Transport mode: sse or stdio")
	logLevel        = flag.String("log-level", "", "Logging level: debug, info, warn, error, fatal")
	logFile         = flag.String("log-file", "", "Log file path (default: stdout)")
	version         = flag.Bool("version", false, "Show version information and exit")
	aiModel         = flag.String("ai-model", "", "AI model to use for AI tasks (default: gpt-4o)")
	aiMaxIterations = flag.Int("ai-max-iterations", 0, "Maximum iterations for tool-enabled AI tasks (default: 20)")
	mcpConfigPath   = flag.String("mcp-config-path", "", "Path to MCP configuration file (default: ~/.cursor/mcp.json)")
	dbPath          = flag.String("db-path", "", "Path to SQLite database file")
	disableDB       = flag.Bool("disable-db", false, "Disable database persistence")

	// AI provider flags
	aiProvider      = flag.String("ai-provider", "", "AI provider: ollama, openrouter, openai (default: ollama)")
	ollamaHost      = flag.String("ollama-host", "", "Ollama host (default: desk)")
	ollamaPort      = flag.Int("ollama-port", 0, "Ollama port (default: 11434)")
	ollamaModel     = flag.String("ollama-model", "", "Ollama model (default: qwen3:14b)")
	openrouterKey   = flag.String("openrouter-key", "", "OpenRouter API key")
	openrouterModel = flag.String("openrouter-model", "", "OpenRouter model (default: anthropic/claude-3.5-sonnet)")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg := loadConfig()

	// Show version and exit if requested
	if *version {
		log.Printf("%s version %s", cfg.Server.Name, cfg.Server.Version)
		os.Exit(0)
	}

	// Create a context that will be cancelled on interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the application
	app, err := createApp(cfg)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Start the application
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	// Wait for termination signal
	waitForSignal(cancel, app)
}

// loadConfig loads configuration from environment and command line flags
func loadConfig() *config.Config {
	// Start with defaults
	cfg := config.DefaultConfig()

	// Override with environment variables
	config.FromEnv(cfg)

	// Override with command-line flags
	applyCommandLineFlagsToConfig(cfg)

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	return cfg
}

// applyCommandLineFlagsToConfig applies command line flags to the configuration
func applyCommandLineFlagsToConfig(cfg *config.Config) {
	if *address != "" {
		cfg.Server.Address = *address
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *transport != "" {
		cfg.Server.TransportMode = *transport
	}
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}
	if *logFile != "" {
		cfg.Logging.FilePath = *logFile
	}
	if *aiModel != "" {
		cfg.AI.Model = *aiModel
	}
	if *aiMaxIterations > 0 {
		cfg.AI.MaxToolIterations = *aiMaxIterations
	}
	if *mcpConfigPath != "" {
		cfg.AI.MCPConfigFilePath = *mcpConfigPath
	}
	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	if *disableDB {
		cfg.Database.Enabled = false
	}

	// AI provider flags
	if *aiProvider != "" {
		cfg.AI.Provider = *aiProvider
	}
	if *ollamaHost != "" {
		cfg.AI.OllamaHost = *ollamaHost
	}
	if *ollamaPort != 0 {
		cfg.AI.OllamaPort = *ollamaPort
	}
	if *ollamaModel != "" {
		cfg.AI.OllamaModel = *ollamaModel
	}
	if *openrouterKey != "" {
		cfg.AI.OpenRouterAPIKey = *openrouterKey
	}
	if *openrouterModel != "" {
		cfg.AI.OpenRouterModel = *openrouterModel
	}
}

// Application represents the running application
type Application struct {
	scheduler     *scheduler.Scheduler
	cmdExecutor   *command.CommandExecutor
	agentExecutor *agent.AgentExecutor
	server        *server.MCPServer
	logger        *logging.Logger
	storage       *storage.SQLiteStorage
	dbPath        string // Store the database path for logging
}

// createApp creates a new application instance
func createApp(cfg *config.Config) (*Application, error) {
	// Create components
	cmdExec := command.NewCommandExecutor()
	agentExec := agent.NewAgentExecutor(cfg)
	sched := scheduler.NewScheduler(&cfg.Scheduler)

	// Initialize storage if enabled
	var sqliteStorage *storage.SQLiteStorage
	var dbPath string
	if cfg.Database.Enabled {
		var err error
		dbPath = cfg.Database.Path
		sqliteStorage, err = storage.NewSQLiteStorage(dbPath)
		if err != nil {
			return nil, err
		}

		// Set storage for the scheduler
		if err := sched.SetStorage(sqliteStorage); err != nil {
			sqliteStorage.Close()
			return nil, err
		}
	}

	// Create the MCP server
	mcpServer, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec)
	if err != nil {
		if sqliteStorage != nil {
			sqliteStorage.Close()
		}
		return nil, err
	}

	// Get the default logger that was configured by the server
	logger := logging.GetDefaultLogger()

	// Create the application
	app := &Application{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		server:        mcpServer,
		logger:        logger,
		storage:       sqliteStorage,
		dbPath:        dbPath,
	}

	return app, nil
}

// Start starts the application
func (a *Application) Start(ctx context.Context) error {
	// Start the scheduler
	a.scheduler.Start(ctx)
	a.logger.Infof("Task scheduler started")

	if a.storage != nil {
		a.logger.Infof("SQLite persistence enabled at: %s", a.dbPath)
	}

	// Start the MCP server
	if err := a.server.Start(ctx); err != nil {
		return err
	}
	a.logger.Infof("MCP server started")

	return nil
}

// Stop stops the application
func (a *Application) Stop() error {
	// Stop the scheduler
	err := a.scheduler.Stop()
	if err != nil {
		return err
	}
	a.logger.Infof("Task scheduler stopped")

	// Stop the server
	if err := a.server.Stop(); err != nil {
		a.logger.Errorf("Error stopping MCP server: %v", err)
		return err
	}
	a.logger.Infof("MCP server stopped")

	// Close storage
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			a.logger.Errorf("Error closing storage: %v", err)
			return err
		}
		a.logger.Infof("Storage closed")
	}

	return nil
}

// waitForSignal waits for termination signals and performs cleanup
func waitForSignal(cancel context.CancelFunc, app *Application) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	<-signalCh
	app.logger.Infof("Received termination signal, shutting down...")

	// Cancel the context to initiate shutdown
	cancel()

	// Stop the application with a timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	shutdownDone := make(chan struct{})
	go func() {
		if err := app.Stop(); err != nil {
			app.logger.Errorf("Error during shutdown: %v", err)
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		app.logger.Infof("Graceful shutdown completed")
	case <-shutdownCtx.Done():
		app.logger.Warnf("Shutdown timed out")
	}
}
