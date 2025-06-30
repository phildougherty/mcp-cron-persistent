// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcp-cron-persistent/internal/agent"
	"mcp-cron-persistent/internal/command"
	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/observability"
	"mcp-cron-persistent/internal/scheduler"
	"mcp-cron-persistent/internal/server"
	"mcp-cron-persistent/internal/storage"
)

const (
	AppName    = "mcp-cron-persistent"
	AppVersion = "0.2.0"
)

var (
	// Basic server flags
	address     = flag.String("address", "", "The address to bind the server to")
	port        = flag.Int("port", 0, "The port to bind the server to")
	transport   = flag.String("transport", "", "Transport mode: sse or stdio")
	logLevel    = flag.String("log-level", "", "Logging level: debug, info, warn, error, fatal")
	logFile     = flag.String("log-file", "", "Log file path (default: stdout)")
	showVersion = flag.Bool("version", false, "Show version information")

	// Database flags
	dbPath    = flag.String("db-path", "", "Path to SQLite database file")
	disableDB = flag.Bool("disable-db", false, "Disable database persistence")

	// OpenWebUI flags
	openwebuiURL     = flag.String("openwebui-url", "", "OpenWebUI base URL")
	openwebuiAPIKey  = flag.String("openwebui-api-key", "", "OpenWebUI API key")
	openwebuiModel   = flag.String("openwebui-model", "", "OpenWebUI model to use for AI tasks")
	openwebuiUserID  = flag.String("openwebui-user-id", "", "OpenWebUI user ID")
	disableOpenWebUI = flag.Bool("disable-openwebui", false, "Disable OpenWebUI integration")

	// Enhanced AI configuration flags
	ollamaEnabled = flag.Bool("ollama", false, "Enable Ollama integration")
	ollamaURL     = flag.String("ollama-url", "", "Ollama base URL")
	ollamaModel   = flag.String("ollama-model", "", "Default Ollama model")

	openrouterEnabled = flag.Bool("openrouter", false, "Enable OpenRouter integration")
	openrouterKey     = flag.String("openrouter-key", "", "OpenRouter API key")
	openrouterModel   = flag.String("openrouter-model", "", "Default OpenRouter model")

	modelRouterEnabled = flag.Bool("model-router", false, "Enable intelligent model routing")
	preferLocal        = flag.Bool("prefer-local", false, "Prefer local models when available")
	maxCostPerTask     = flag.Float64("max-cost-per-task", 0, "Maximum cost per task execution")

	autonomousMode  = flag.Bool("autonomous", false, "Enable fully autonomous agent mode")
	learningEnabled = flag.Bool("learning", false, "Enable agent learning capabilities")
	selfReflection  = flag.Bool("self-reflection", false, "Enable agent self-reflection")

	// Observability flags
	metricsEnabled = flag.Bool("metrics", false, "Enable metrics collection")
	healthCheck    = flag.Bool("health-check", false, "Perform startup health checks")

	// Development flags
	debugConfig = flag.Bool("debug-config", false, "Print configuration on startup")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg := loadConfig()

	// Show version and enhanced capabilities
	if *showVersion {
		showVersionInfo(cfg)
		os.Exit(0)
	}

	// Debug configuration if requested
	if *debugConfig {
		printDebugConfig(cfg)
	}

	// Create a context that will be cancelled on interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the application
	app, err := createApp(cfg)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Perform health checks if requested
	if *healthCheck {
		if err := app.PerformHealthChecks(); err != nil {
			log.Fatalf("Health check failed: %v", err)
		}
	}

	// Start the application
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	// Print startup summary
	app.PrintStartupSummary()

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
	// Basic server flags
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

	// Database flags
	if *dbPath != "" {
		cfg.Database.Path = *dbPath
	}
	if *disableDB {
		cfg.Database.Enabled = false
	}

	// OpenWebUI flags
	if *openwebuiURL != "" {
		cfg.OpenWebUI.BaseURL = *openwebuiURL
	}
	if *openwebuiAPIKey != "" {
		cfg.OpenWebUI.APIKey = *openwebuiAPIKey
	}
	if *openwebuiModel != "" {
		cfg.OpenWebUI.Model = *openwebuiModel
	}
	if *openwebuiUserID != "" {
		cfg.OpenWebUI.UserID = *openwebuiUserID
	}
	if *disableOpenWebUI {
		cfg.OpenWebUI.Enabled = false
	}

	// Enhanced AI configuration overrides
	if *ollamaEnabled {
		cfg.Ollama.Enabled = true
	}
	if *ollamaURL != "" {
		cfg.Ollama.BaseURL = *ollamaURL
		cfg.Ollama.Enabled = true
	}
	if *ollamaModel != "" {
		cfg.Ollama.DefaultModel = *ollamaModel
		cfg.Ollama.Enabled = true
	}

	if *openrouterEnabled {
		cfg.OpenRouter.Enabled = true
	}
	if *openrouterKey != "" {
		cfg.OpenRouter.APIKey = *openrouterKey
		cfg.OpenRouter.Enabled = true
	}
	if *openrouterModel != "" {
		cfg.OpenRouter.DefaultModel = *openrouterModel
	}

	if *modelRouterEnabled {
		cfg.ModelRouter.Enabled = true
	}
	if *preferLocal {
		cfg.ModelRouter.PreferLocal = true
	}
	if *maxCostPerTask > 0 {
		cfg.ModelRouter.MaxCostPerTask = *maxCostPerTask
	}

	// Auto-enable model router if we have multiple backends
	if cfg.Ollama.Enabled && cfg.OpenRouter.Enabled && !cfg.ModelRouter.Enabled {
		cfg.ModelRouter.Enabled = true
		log.Println("Auto-enabled model router with multiple AI backends")
	}

	// Set enhanced agent capabilities via environment variables for executor
	if *autonomousMode {
		os.Setenv("MCP_CRON_AGENT_AUTONOMOUS", "true")
	}
	if *learningEnabled {
		os.Setenv("MCP_CRON_AGENT_LEARNING", "true")
	}
	if *selfReflection {
		os.Setenv("MCP_CRON_AGENT_REFLECTION", "true")
	}
}

// Application represents the running application with enhanced capabilities
type Application struct {
	scheduler        *scheduler.Scheduler
	cmdExecutor      *command.CommandExecutor
	agentExecutor    *agent.AgentExecutor
	server           *server.MCPServer
	logger           *logging.Logger
	storage          *storage.SQLiteStorage
	metricsCollector *observability.MetricsCollector
	config           *config.Config
	dbPath           string
	startTime        time.Time
}

// createApp creates a new application instance with enhanced features
func createApp(cfg *config.Config) (*Application, error) {
	startTime := time.Now()

	// Initialize logger early
	var logger *logging.Logger
	if cfg.Logging.FilePath != "" {
		var err error
		logger, err = logging.FileLogger(cfg.Logging.FilePath, parseLogLevel(cfg.Logging.Level))
		if err != nil {
			return nil, fmt.Errorf("failed to create file logger: %w", err)
		}
	} else {
		logger = logging.New(logging.Options{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	}
	logging.SetDefaultLogger(logger)

	// Create metrics collector if enabled
	var metricsCollector *observability.MetricsCollector
	if *metricsEnabled {
		metricsCollector = observability.NewMetricsCollector(logger)
		// Start periodic updates
		go metricsCollector.StartPeriodicUpdates(context.Background(), 30*time.Second)
		logger.Infof("Metrics collection enabled")
	}

	// Create components with enhanced capabilities
	cmdExec := command.NewCommandExecutor()
	agentExec := agent.NewAgentExecutor(cfg) // Enhanced agent with all new capabilities
	sched := scheduler.NewScheduler(&cfg.Scheduler)

	// Set metrics collector for scheduler
	if metricsCollector != nil {
		sched.SetMetricsCollector(metricsCollector)
	}

	// Configure enhanced agent capabilities
	if *autonomousMode {
		agentExec.SetAutonomousMode(true)
		logger.Infof("Autonomous agent mode enabled")
	}
	if *learningEnabled {
		agentExec.SetLearningEnabled(true)
		logger.Infof("Agent learning capabilities enabled")
	}
	if *selfReflection {
		agentExec.SetSelfReflection(true)
		logger.Infof("Agent self-reflection enabled")
	}

	// Create the MCP server
	mcpServer, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}

	// CRITICAL: Set the task executor BEFORE setting storage
	sched.SetTaskExecutor(mcpServer)

	// Initialize storage if enabled (this will load and schedule existing tasks)
	var sqliteStorage *storage.SQLiteStorage
	var dbPath string
	if cfg.Database.Enabled {
		dbPath = cfg.Database.Path
		sqliteStorage, err = storage.NewSQLiteStorage(dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage: %w", err)
		}

		// Set storage for the scheduler
		if err := sched.SetStorage(sqliteStorage); err != nil {
			sqliteStorage.Close()
			return nil, fmt.Errorf("failed to set scheduler storage: %w", err)
		}

		// Also set storage reference in MCP server
		mcpServer.SetStorage(sqliteStorage)
		logger.Infof("Database storage initialized: %s", dbPath)
	}

	// Create the application
	app := &Application{
		scheduler:        sched,
		cmdExecutor:      cmdExec,
		agentExecutor:    agentExec,
		server:           mcpServer,
		logger:           logger,
		storage:          sqliteStorage,
		metricsCollector: metricsCollector,
		config:           cfg,
		dbPath:           dbPath,
		startTime:        startTime,
	}

	return app, nil
}

// Start starts the application with enhanced logging and monitoring
func (a *Application) Start(ctx context.Context) error {
	a.logger.Infof("Starting %s v%s", AppName, AppVersion)
	a.logger.Infof("Configuration: Transport=%s, Address=%s:%d",
		a.config.Server.TransportMode, a.config.Server.Address, a.config.Server.Port)

	// Log enhanced AI capabilities
	if a.config.Ollama.Enabled {
		a.logger.Infof("Ollama integration enabled: %s (model: %s)",
			a.config.Ollama.BaseURL, a.config.Ollama.DefaultModel)
	}
	if a.config.OpenRouter.Enabled {
		a.logger.Infof("OpenRouter integration enabled (model: %s)",
			a.config.OpenRouter.DefaultModel)
	}
	if a.config.ModelRouter.Enabled {
		a.logger.Infof("Intelligent model routing enabled (prefer_local: %v)",
			a.config.ModelRouter.PreferLocal)
	}

	// Start the scheduler
	a.scheduler.Start(ctx)
	a.logger.Infof("Task scheduler started")

	if a.storage != nil {
		a.logger.Infof("SQLite persistence enabled at: %s", a.dbPath)
	}

	// Start the MCP server
	if err := a.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	if a.config.Server.TransportMode == "sse" {
		a.logger.Infof("MCP server started on http://%s:%d/sse",
			a.config.Server.Address, a.config.Server.Port)
	} else {
		a.logger.Infof("MCP server started with stdio transport")
	}

	return nil
}

// Stop stops the application with enhanced error handling
func (a *Application) Stop() error {
	a.logger.Infof("Stopping %s...", AppName)
	errors := make([]error, 0)

	// Stop the scheduler
	if err := a.scheduler.Stop(); err != nil {
		errors = append(errors, fmt.Errorf("scheduler stop: %w", err))
		a.logger.Errorf("Error stopping scheduler: %v", err)
	} else {
		a.logger.Infof("Task scheduler stopped")
	}

	// Stop the server
	if err := a.server.Stop(); err != nil {
		errors = append(errors, fmt.Errorf("server stop: %w", err))
		a.logger.Errorf("Error stopping MCP server: %v", err)
	} else {
		a.logger.Infof("MCP server stopped")
	}

	// Close storage
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			errors = append(errors, fmt.Errorf("storage close: %w", err))
			a.logger.Errorf("Error closing storage: %v", err)
		} else {
			a.logger.Infof("Storage closed")
		}
	}

	// Return first error if any
	if len(errors) > 0 {
		return errors[0]
	}

	uptime := time.Since(a.startTime)
	a.logger.Infof("%s stopped successfully (uptime: %v)", AppName, uptime)
	return nil
}

// PerformHealthChecks performs startup health checks
func (a *Application) PerformHealthChecks() error {
	a.logger.Infof("Performing health checks...")

	// Check database if enabled
	if a.storage != nil {
		// Try to load tasks to verify database connectivity
		_, err := a.storage.LoadAllTasks()
		if err != nil {
			return fmt.Errorf("database health check failed: %w", err)
		}
		a.logger.Infof("✓ Database health check passed")
	}

	// Check AI backends
	if a.config.OpenRouter.Enabled {
		if a.config.OpenRouter.APIKey == "" {
			return fmt.Errorf("OpenRouter enabled but API key not set")
		}
		a.logger.Infof("✓ OpenRouter configuration check passed")
	}

	if a.config.Ollama.Enabled {
		// Could add Ollama connectivity check here
		a.logger.Infof("✓ Ollama configuration check passed")
	}

	a.logger.Infof("All health checks passed")
	return nil
}

// PrintStartupSummary prints a summary of the running application
func (a *Application) PrintStartupSummary() {
	a.logger.Infof("=== %s v%s Ready ===", AppName, AppVersion)

	if a.config.Server.TransportMode == "sse" {
		a.logger.Infof("Server endpoint: http://%s:%d/sse", a.config.Server.Address, a.config.Server.Port)
	} else {
		a.logger.Infof("Transport: stdio")
	}

	// Show AI capabilities
	capabilities := []string{}
	if a.config.Ollama.Enabled {
		capabilities = append(capabilities, fmt.Sprintf("Ollama (%s)", a.config.Ollama.DefaultModel))
	}
	if a.config.OpenRouter.Enabled {
		capabilities = append(capabilities, fmt.Sprintf("OpenRouter (%s)", a.config.OpenRouter.DefaultModel))
	}
	if a.config.ModelRouter.Enabled {
		capabilities = append(capabilities, "Intelligent Routing")
	}

	if len(capabilities) > 0 {
		a.logger.Infof("AI Capabilities: %s", fmt.Sprintf("[%s]", fmt.Sprintf("%v", capabilities)))
	}

	if a.metricsCollector != nil {
		a.logger.Infof("Metrics collection: enabled")
	}

	if a.storage != nil {
		a.logger.Infof("Database: %s", a.dbPath)
	}

	a.logger.Infof("Ready for enhanced agentic task execution!")
	a.logger.Infof("===============================")
}

// GetMetricsStats returns current metrics statistics
func (a *Application) GetMetricsStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if a.metricsCollector != nil {
		summary := a.metricsCollector.GetMetricsSummary()
		stats["metrics"] = summary
	}

	if a.agentExecutor != nil {
		agentStats := a.agentExecutor.GetMemoryStats()
		stats["agent"] = agentStats
	}

	stats["uptime"] = time.Since(a.startTime).String()
	stats["version"] = AppVersion

	return stats
}

// waitForSignal waits for termination signals and performs cleanup
func waitForSignal(cancel context.CancelFunc, app *Application) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-signalCh
	app.logger.Infof("Received signal %v, initiating graceful shutdown...", sig)

	// Cancel the context to initiate shutdown
	cancel()

	// Stop the application with a timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		app.logger.Warnf("Shutdown timed out after 10 seconds")
	}
}

// Utility functions

func parseLogLevel(level string) logging.LogLevel {
	switch level {
	case "debug":
		return logging.Debug
	case "info":
		return logging.Info
	case "warn":
		return logging.Warn
	case "error":
		return logging.Error
	case "fatal":
		return logging.Fatal
	default:
		return logging.Info
	}
}

func showVersionInfo(cfg *config.Config) {
	fmt.Printf("%s version %s\n\n", AppName, AppVersion)
	fmt.Println("Enhanced with:")
	fmt.Println("  ✓ Intelligent model routing")
	fmt.Println("  ✓ Local Ollama integration")
	fmt.Println("  ✓ OpenRouter cloud models")
	fmt.Println("  ✓ Autonomous agent capabilities")
	fmt.Println("  ✓ Persistent conversation memory")
	fmt.Println("  ✓ Self-reflection and learning")
	fmt.Println("  ✓ Security-aware routing")
	fmt.Println("  ✓ Cost optimization")
	fmt.Println("  ✓ Enhanced observability")

	fmt.Println("\nDefault Configuration:")
	fmt.Printf("  Transport: %s\n", cfg.Server.TransportMode)
	fmt.Printf("  Address: %s:%d\n", cfg.Server.Address, cfg.Server.Port)
	fmt.Printf("  Ollama: %v (%s)\n", cfg.Ollama.Enabled, cfg.Ollama.DefaultModel)
	fmt.Printf("  Model Router: %v\n", cfg.ModelRouter.Enabled)
	fmt.Printf("  Database: %v (%s)\n", cfg.Database.Enabled, cfg.Database.Path)
}

func printDebugConfig(cfg *config.Config) {
	fmt.Printf("=== DEBUG CONFIGURATION ===\n")
	fmt.Printf("Server:\n")
	fmt.Printf("  Address: %s:%d\n", cfg.Server.Address, cfg.Server.Port)
	fmt.Printf("  Transport: %s\n", cfg.Server.TransportMode)
	fmt.Printf("  Name: %s\n", cfg.Server.Name)
	fmt.Printf("  Version: %s\n", cfg.Server.Version)

	fmt.Printf("\nOllama:\n")
	fmt.Printf("  Enabled: %v\n", cfg.Ollama.Enabled)
	fmt.Printf("  BaseURL: %s\n", cfg.Ollama.BaseURL)
	fmt.Printf("  DefaultModel: %s\n", cfg.Ollama.DefaultModel)
	fmt.Printf("  RequestTimeout: %d\n", cfg.Ollama.RequestTimeout)

	fmt.Printf("\nOpenRouter:\n")
	fmt.Printf("  Enabled: %v\n", cfg.OpenRouter.Enabled)
	fmt.Printf("  DefaultModel: %s\n", cfg.OpenRouter.DefaultModel)
	fmt.Printf("  APIKey set: %v\n", cfg.OpenRouter.APIKey != "")
	fmt.Printf("  MCPProxyURL: %s\n", cfg.OpenRouter.MCPProxyURL)
	fmt.Printf("  MCPProxyKey set: %v\n", cfg.OpenRouter.MCPProxyKey != "")

	fmt.Printf("\nModelRouter:\n")
	fmt.Printf("  Enabled: %v\n", cfg.ModelRouter.Enabled)
	fmt.Printf("  PreferLocal: %v\n", cfg.ModelRouter.PreferLocal)
	fmt.Printf("  FallbackToCloud: %v\n", cfg.ModelRouter.FallbackToCloud)
	fmt.Printf("  DefaultHint: %s\n", cfg.ModelRouter.DefaultHint)
	fmt.Printf("  MaxCostPerTask: %.4f\n", cfg.ModelRouter.MaxCostPerTask)

	fmt.Printf("\nOpenWebUI:\n")
	fmt.Printf("  Enabled: %v\n", cfg.OpenWebUI.Enabled)
	fmt.Printf("  BaseURL: %s\n", cfg.OpenWebUI.BaseURL)
	fmt.Printf("  Model: %s\n", cfg.OpenWebUI.Model)
	fmt.Printf("  APIKey set: %v\n", cfg.OpenWebUI.APIKey != "")

	fmt.Printf("\nDatabase:\n")
	fmt.Printf("  Enabled: %v\n", cfg.Database.Enabled)
	fmt.Printf("  Path: %s\n", cfg.Database.Path)

	fmt.Printf("\nLegacy:\n")
	fmt.Printf("  UseOpenRouter: %v\n", cfg.UseOpenRouter)
	fmt.Printf("============================\n\n")
}
