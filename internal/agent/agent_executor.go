// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"mcp-cron-persistent/internal/config"
	"mcp-cron-persistent/internal/logging"
	"mcp-cron-persistent/internal/model"
	"mcp-cron-persistent/internal/model_router"
	"mcp-cron-persistent/internal/openrouter"
	"mcp-cron-persistent/internal/openwebui"
)

// ContextKey is a custom type for context keys to avoid collisions
type ContextKey string

const (
	TaskIDKey ContextKey = "task_id"
)

// AgentExecutor executes AI tasks with enhanced agentic capabilities
type AgentExecutor struct {
	mu               sync.RWMutex
	results          map[string]*model.Result // Map of taskID -> Result
	config           *config.Config
	openwebuiClient  *openwebui.Client
	openrouterClient *openrouter.Client
	modelRouter      *model_router.ModelRouter
	toolProxy        *openrouter.ToolProxy
	logger           *logging.Logger
	// Enhanced agentic features
	conversationMemory map[string]*ConversationMemory
	memoryMutex        sync.RWMutex
	learningEnabled    bool
	selfReflection     bool
	autonomousMode     bool
	// Intelligence features
	taskAnalyzer       *TaskAnalyzer
	securityClassifier *SecurityClassifier
}

// ConversationMemory stores conversation state and learning
type ConversationMemory struct {
	TaskID          string                 `json:"task_id"`
	ConversationID  string                 `json:"conversation_id"`
	Messages        []ConversationMessage  `json:"messages"`
	Summary         string                 `json:"summary"`
	LearningNotes   []string               `json:"learning_notes"`
	SuccessPatterns []string               `json:"success_patterns"`
	FailurePatterns []string               `json:"failure_patterns"`
	LastUpdated     time.Time              `json:"last_updated"`
	ExecutionCount  int                    `json:"execution_count"`
	SuccessCount    int                    `json:"success_count"`
	Tools           []string               `json:"tools_used"`
	Context         map[string]interface{} `json:"context"`
	SecurityLevel   string                 `json:"security_level"`
	ModelPreference string                 `json:"model_preference"`
}

type ConversationMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	ToolCalls []string  `json:"tool_calls,omitempty"`
	ModelUsed string    `json:"model_used,omitempty"`
}

// TaskAnalyzer analyzes tasks for complexity, security, and requirements
type TaskAnalyzer struct {
	logger *logging.Logger
}

// SecurityClassifier classifies tasks for security sensitivity
type SecurityClassifier struct {
	logger *logging.Logger
}

// TaskAnalysis contains the analysis results for a task
type TaskAnalysis struct {
	Complexity        string   `json:"complexity"`        // low, medium, high
	SecurityLevel     string   `json:"security_level"`    // public, confidential, secret
	RequiredFeatures  []string `json:"required_features"` // tool_use, vision, etc.
	EstimatedCost     float64  `json:"estimated_cost"`
	PreferredProvider string   `json:"preferred_provider"` // openrouter, ollama, openwebui
	Reasoning         string   `json:"reasoning"`
	UseLocal          bool     `json:"use_local"`
}

// NewAgentExecutor creates a new agent executor with enhanced capabilities
func NewAgentExecutor(cfg *config.Config) *AgentExecutor {
	logger := logging.GetDefaultLogger()

	executor := &AgentExecutor{
		results:            make(map[string]*model.Result),
		config:             cfg,
		conversationMemory: make(map[string]*ConversationMemory),
		logger:             logger,
		learningEnabled:    true,
		selfReflection:     true,
		autonomousMode:     true,
		taskAnalyzer:       &TaskAnalyzer{logger: logger},
		securityClassifier: &SecurityClassifier{logger: logger},
	}

	// Initialize OpenWebUI client if enabled
	if cfg.OpenWebUI.Enabled {
		executor.openwebuiClient = openwebui.NewClient(&cfg.OpenWebUI, logger)
	}

	// Initialize OpenRouter client if enabled
	if cfg.OpenRouter.Enabled && cfg.OpenRouter.APIKey != "" {
		executor.openrouterClient = openrouter.NewClient(cfg.OpenRouter.APIKey, logger)

		// Initialize tool proxy for MCP integration
		if cfg.OpenRouter.MCPProxyURL != "" {
			executor.toolProxy = openrouter.NewToolProxy(cfg.OpenRouter.MCPProxyURL, cfg.OpenRouter.MCPProxyKey)

			// Load tools asynchronously
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := executor.toolProxy.LoadTools(ctx); err != nil {
					logger.Warnf("Failed to load MCP tools: %v", err)
				} else {
					tools := executor.toolProxy.GetTools()
					logger.Infof("Loaded %d MCP tools for agentic tasks", len(tools))
				}
			}()
		}
	}

	// Initialize model router if enabled
	if cfg.ModelRouter.Enabled {
		gatewayURL := cfg.OpenRouter.MCPProxyURL
		if gatewayURL == "" {
			gatewayURL = "http://localhost:3001" // Default MCP gateway
		}
		executor.modelRouter = model_router.NewModelRouter(gatewayURL)

		// Load Ollama models if enabled
		if cfg.Ollama.Enabled {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := executor.modelRouter.LoadOllamaModels(ctx, cfg.Ollama.BaseURL); err != nil {
					logger.Warnf("Failed to load Ollama models: %v", err)
				} else {
					logger.Infof("Successfully loaded Ollama models for intelligent routing")
				}
			}()
		}
	}

	return executor
}

// Execute implements the Task execution for the scheduler with enhanced agentic capabilities
func (ae *AgentExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Runtime validation only checks fields needed for execution (ID and Prompt)
	if task.ID == "" || task.Prompt == "" {
		return fmt.Errorf("invalid task: missing ID or Prompt")
	}

	ae.logger.Infof("Executing enhanced agentic AI task: %s", task.ID)

	// Load or create conversation memory
	memory := ae.getOrCreateMemory(task)

	// Execute the task with enhanced capabilities
	result := ae.executeAgentTaskEnhanced(ctx, task.ID, task.Prompt, timeout, task, memory)

	if result.Error != "" {
		return fmt.Errorf(result.Error)
	}
	return nil
}

// executeAgentTaskEnhanced executes a command using enhanced AI agent capabilities
func (ae *AgentExecutor) executeAgentTaskEnhanced(
	ctx context.Context,
	taskID string,
	prompt string,
	timeout time.Duration,
	task *model.Task,
	memory *ConversationMemory,
) *model.Result {
	result := &model.Result{
		Prompt:    prompt,
		StartTime: time.Now(),
		TaskID:    taskID,
	}

	// Store the result
	ae.mu.Lock()
	ae.results[taskID] = result
	ae.mu.Unlock()

	// Create a context with timeout and task ID
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	execCtx = context.WithValue(execCtx, TaskIDKey, taskID)

	// Analyze the task for intelligent routing
	analysis := ae.analyzeTask(task, memory)

	// Execute the task using intelligent routing based on analysis
	var output string
	var err error
	var modelUsed string

	switch analysis.PreferredProvider {
	case "ollama":
		output, err = ae.executeWithOllamaLocal(execCtx, task, analysis)
		modelUsed = ae.config.Ollama.DefaultModel
	case "openrouter":
		output, err = ae.executeWithOpenRouterIntelligent(execCtx, task, analysis)
		modelUsed = ae.config.OpenRouter.DefaultModel
	case "openwebui":
		output, err = ae.executeWithOpenWebUI(execCtx, task, analysis)
		modelUsed = ae.config.OpenWebUI.Model
	default:
		// Fallback to standard execution
		output, err = RunTask(execCtx, task, ae.config)
		modelUsed = "standard"
	}

	// Update result fields
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	if task != nil && task.ConversationID != "" {
		result.ConversationID = task.ConversationID
	}

	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 1
		result.Output = fmt.Sprintf("Error executing AI task: %v", err)
	} else {
		result.Output = output
		result.ExitCode = 0
	}

	// Update conversation memory with execution results and model used
	ae.updateMemoryWithResult(task, memory, result, err, modelUsed, analysis)

	// Self-reflection and learning
	if ae.selfReflection && ae.learningEnabled {
		go ae.performSelfReflection(task, memory, result, err)
	}

	// Convert the result to JSON and log it
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		errorJSON, _ := json.Marshal(map[string]string{
			"error":   "marshaling_error",
			"message": err.Error(),
			"task_id": taskID,
		})
		log.Println(string(errorJSON))
	} else {
		log.Println(string(jsonData))
	}

	return result
}

// analyzeTask performs intelligent analysis of the task
func (ae *AgentExecutor) analyzeTask(task *model.Task, memory *ConversationMemory) *TaskAnalysis {
	analysis := &TaskAnalysis{
		Complexity:       ae.taskAnalyzer.AnalyzeComplexity(task.Prompt),
		SecurityLevel:    ae.securityClassifier.ClassifySecurity(task.Prompt, task.Description),
		RequiredFeatures: ae.taskAnalyzer.DetectRequiredFeatures(task.Prompt),
		EstimatedCost:    ae.taskAnalyzer.EstimateCost(task.Prompt),
	}

	// Intelligent provider selection based on analysis
	analysis.PreferredProvider, analysis.UseLocal, analysis.Reasoning = ae.selectOptimalProvider(analysis, memory)

	ae.logger.Debugf("Task analysis for %s: complexity=%s, security=%s, provider=%s, reasoning=%s",
		task.ID, analysis.Complexity, analysis.SecurityLevel, analysis.PreferredProvider, analysis.Reasoning)

	return analysis
}

// selectOptimalProvider intelligently selects the best provider
func (ae *AgentExecutor) selectOptimalProvider(analysis *TaskAnalysis, memory *ConversationMemory) (string, bool, string) {
	// Force local execution for confidential/secret information
	if analysis.SecurityLevel == "confidential" || analysis.SecurityLevel == "secret" {
		if ae.config.Ollama.Enabled {
			return "ollama", true, "confidential data requires local processing"
		}
		return "openwebui", true, "confidential data requires local processing (OpenWebUI fallback)"
	}

	// Prefer OpenRouter for most tasks (default intelligent behavior)
	if ae.config.OpenRouter.Enabled && ae.config.OpenRouter.APIKey != "" {
		// Use OpenRouter for complex tasks and when tools are needed
		if analysis.Complexity == "high" || contains(analysis.RequiredFeatures, "tool_use") {
			return "openrouter", false, "complex task benefits from cloud model with tools"
		}

		// Use OpenRouter for medium complexity if under cost threshold
		if analysis.Complexity == "medium" && analysis.EstimatedCost <= ae.config.ModelRouter.MaxCostPerTask {
			return "openrouter", false, "medium complexity task within cost threshold"
		}
	}

	// Use local Ollama for simple tasks or when cost-conscious
	if ae.config.Ollama.Enabled {
		if analysis.Complexity == "low" {
			return "ollama", true, "simple task suitable for local model"
		}

		if analysis.EstimatedCost > ae.config.ModelRouter.MaxCostPerTask {
			return "ollama", true, "cost optimization requires local model"
		}
	}

	// Fallback based on success history
	if memory.ExecutionCount > 0 {
		successRate := float64(memory.SuccessCount) / float64(memory.ExecutionCount)
		if successRate < 0.7 && ae.config.OpenRouter.Enabled {
			return "openrouter", false, "low success rate suggests need for more capable model"
		}
	}

	// Final fallback
	if ae.config.OpenRouter.Enabled {
		return "openrouter", false, "default preference for cloud model"
	}
	if ae.config.Ollama.Enabled {
		return "ollama", true, "fallback to local model"
	}

	return "openwebui", true, "final fallback to OpenWebUI"
}

// executeWithOllamaLocal executes with local Ollama
func (ae *AgentExecutor) executeWithOllamaLocal(ctx context.Context, task *model.Task, analysis *TaskAnalysis) (string, error) {
	ae.logger.Infof("Executing task with local Ollama: %s (reason: %s)", task.ID, analysis.Reasoning)
	return tryOllamaExecution(ctx, task, ae.config)
}

// executeWithOpenRouterIntelligent executes with OpenRouter with intelligent features
func (ae *AgentExecutor) executeWithOpenRouterIntelligent(ctx context.Context, task *model.Task, analysis *TaskAnalysis) (string, error) {
	ae.logger.Infof("Executing task with OpenRouter: %s (reason: %s)", task.ID, analysis.Reasoning)

	model := task.Model
	if model == "" {
		model = ae.selectModelForComplexity(analysis.Complexity)
	}

	return tryOpenRouterExecution(ctx, task, model, ae.config)
}

// executeWithOpenWebUI executes with OpenWebUI
func (ae *AgentExecutor) executeWithOpenWebUI(ctx context.Context, task *model.Task, analysis *TaskAnalysis) (string, error) {
	ae.logger.Infof("Executing task with OpenWebUI: %s (reason: %s)", task.ID, analysis.Reasoning)
	return runTaskWithOpenWebUI(ctx, task, ae.config, ae.logger)
}

// selectModelForComplexity selects the appropriate model based on task complexity
func (ae *AgentExecutor) selectModelForComplexity(complexity string) string {
	switch complexity {
	case "high":
		return "anthropic/claude-sonnet-4"
	case "medium":
		return "anthropic/claude-3.7-sonnet"
	case "low":
		return "openai/gpt-4o-mini"
	default:
		return ae.config.OpenRouter.DefaultModel
	}
}

// Helper functions for task analysis
func (ta *TaskAnalyzer) AnalyzeComplexity(prompt string) string {
	prompt = strings.ToLower(prompt)

	highComplexityKeywords := []string{
		"analyze", "research", "complex", "comprehensive", "detailed analysis",
		"machine learning", "algorithm", "optimization", "statistical",
		"academic", "scientific", "mathematical", "advanced",
	}

	mediumComplexityKeywords := []string{
		"summary", "explain", "compare", "describe", "outline",
		"plan", "strategy", "design", "create", "develop",
	}

	for _, keyword := range highComplexityKeywords {
		if strings.Contains(prompt, keyword) {
			return "high"
		}
	}

	for _, keyword := range mediumComplexityKeywords {
		if strings.Contains(prompt, keyword) {
			return "medium"
		}
	}

	if len(prompt) > 500 {
		return "medium"
	}

	return "low"
}

func (sc *SecurityClassifier) ClassifySecurity(prompt, description string) string {
	content := strings.ToLower(prompt + " " + description)

	secretKeywords := []string{
		"password", "api key", "secret", "token", "credential",
		"private key", "confidential", "classified", "sensitive",
		"internal", "proprietary", "personal data", "pii",
	}

	for _, keyword := range secretKeywords {
		if strings.Contains(content, keyword) {
			return "confidential"
		}
	}

	return "public"
}

func (ta *TaskAnalyzer) DetectRequiredFeatures(prompt string) []string {
	prompt = strings.ToLower(prompt)
	features := []string{}

	if strings.Contains(prompt, "tool") || strings.Contains(prompt, "function") ||
		strings.Contains(prompt, "api") || strings.Contains(prompt, "execute") ||
		strings.Contains(prompt, "create") || strings.Contains(prompt, "manage") {
		features = append(features, "tool_use")
	}

	if strings.Contains(prompt, "image") || strings.Contains(prompt, "picture") ||
		strings.Contains(prompt, "photo") || strings.Contains(prompt, "visual") {
		features = append(features, "vision")
	}

	return features
}

func (ta *TaskAnalyzer) EstimateCost(prompt string) float64 {
	baseTokens := len(strings.Fields(prompt)) * 4 / 3 // Rough token estimation

	complexity := ta.AnalyzeComplexity(prompt)

	switch complexity {
	case "high":
		return float64(baseTokens) / 1000.0 * 0.015 // High-end model pricing
	case "medium":
		return float64(baseTokens) / 1000.0 * 0.003 // Mid-tier model pricing
	default:
		return float64(baseTokens) / 1000.0 * 0.0006 // Low-cost model pricing
	}
}

// Memory management methods
func (ae *AgentExecutor) getOrCreateMemory(task *model.Task) *ConversationMemory {
	ae.memoryMutex.Lock()
	defer ae.memoryMutex.Unlock()

	if memory, exists := ae.conversationMemory[task.ID]; exists {
		return memory
	}

	memory := &ConversationMemory{
		TaskID:          task.ID,
		ConversationID:  task.ConversationID,
		Messages:        make([]ConversationMessage, 0),
		LearningNotes:   make([]string, 0),
		SuccessPatterns: make([]string, 0),
		FailurePatterns: make([]string, 0),
		Tools:           make([]string, 0),
		Context:         make(map[string]interface{}),
		LastUpdated:     time.Now(),
		SecurityLevel:   "public",
		ModelPreference: "openrouter", // Default preference
	}

	ae.conversationMemory[task.ID] = memory
	return memory
}

func (ae *AgentExecutor) updateMemoryWithResult(task *model.Task, memory *ConversationMemory, result *model.Result, err error, modelUsed string, analysis *TaskAnalysis) {
	ae.memoryMutex.Lock()
	defer ae.memoryMutex.Unlock()

	memory.ExecutionCount++
	memory.LastUpdated = time.Now()
	memory.SecurityLevel = analysis.SecurityLevel

	// Add execution message to memory
	timestamp := time.Now()
	memory.Messages = append(memory.Messages, ConversationMessage{
		Role:      "user",
		Content:   task.Prompt,
		Timestamp: timestamp,
		ModelUsed: modelUsed,
	})

	if err == nil {
		memory.SuccessCount++
		if result != nil {
			memory.Messages = append(memory.Messages, ConversationMessage{
				Role:      "assistant",
				Content:   result.Output,
				Timestamp: timestamp,
				ModelUsed: modelUsed,
			})
		}

		// Learn successful model preference
		memory.ModelPreference = analysis.PreferredProvider
	} else {
		// Learn from failure
		failurePattern := fmt.Sprintf("Failed execution with %s: %v", modelUsed, err)
		memory.FailurePatterns = append(memory.FailurePatterns, failurePattern)
	}

	// Limit memory size
	maxMessages := 20
	if len(memory.Messages) > maxMessages {
		memory.Messages = memory.Messages[len(memory.Messages)-maxMessages:]
	}
}

// performSelfReflection performs post-execution analysis and learning
func (ae *AgentExecutor) performSelfReflection(task *model.Task, memory *ConversationMemory, result *model.Result, err error) {
	ae.logger.Debugf("Performing self-reflection for task %s", task.ID)

	// Analyze execution patterns
	if err == nil && result != nil {
		// Extract success patterns
		if strings.Contains(result.Output, "successfully") ||
			strings.Contains(result.Output, "completed") ||
			strings.Contains(result.Output, "done") {

			successPattern := fmt.Sprintf("Successful approach for %s complexity with %s",
				ae.taskAnalyzer.AnalyzeComplexity(task.Prompt), memory.ModelPreference)

			ae.memoryMutex.Lock()
			memory.SuccessPatterns = append(memory.SuccessPatterns, successPattern)
			// Limit patterns
			if len(memory.SuccessPatterns) > 10 {
				memory.SuccessPatterns = memory.SuccessPatterns[1:]
			}
			ae.memoryMutex.Unlock()
		}
	}

	// Update task summary if significant progress
	if memory.ExecutionCount%5 == 0 {
		ae.updateMemorySummary(task, memory)
	}
}

func (ae *AgentExecutor) updateMemorySummary(task *model.Task, memory *ConversationMemory) {
	summary := fmt.Sprintf("Task: %s, Executions: %d, Success Rate: %.1f%%, Preferred: %s, Security: %s",
		task.Name,
		memory.ExecutionCount,
		float64(memory.SuccessCount)/float64(memory.ExecutionCount)*100,
		memory.ModelPreference,
		memory.SecurityLevel)

	ae.memoryMutex.Lock()
	memory.Summary = summary
	ae.memoryMutex.Unlock()

	ae.logger.Debugf("Updated memory summary for task %s: %s", task.ID, summary)
}

// Legacy ExecuteAgentTask method for backward compatibility
func (ae *AgentExecutor) ExecuteAgentTask(
	ctx context.Context,
	taskID string,
	prompt string,
	timeout time.Duration,
	task *model.Task,
) *model.Result {
	memory := ae.getOrCreateMemory(task)
	return ae.executeAgentTaskEnhanced(ctx, taskID, prompt, timeout, task, memory)
}

// GetTaskResult implements the ResultProvider interface
func (ae *AgentExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	result, exists := ae.results[taskID]
	return result, exists
}

// StoreResult stores a result for external access
func (ae *AgentExecutor) StoreResult(taskID string, result *model.Result) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.results[taskID] = result
}

// GetConversationMemory retrieves conversation memory for a task
func (ae *AgentExecutor) GetConversationMemory(taskID string) (*ConversationMemory, bool) {
	ae.memoryMutex.RLock()
	defer ae.memoryMutex.RUnlock()
	memory, exists := ae.conversationMemory[taskID]
	return memory, exists
}

// SetLearningEnabled enables or disables learning capabilities
func (ae *AgentExecutor) SetLearningEnabled(enabled bool) {
	ae.learningEnabled = enabled
}

// SetSelfReflection enables or disables self-reflection
func (ae *AgentExecutor) SetSelfReflection(enabled bool) {
	ae.selfReflection = enabled
}

// SetAutonomousMode enables or disables autonomous mode
func (ae *AgentExecutor) SetAutonomousMode(enabled bool) {
	ae.autonomousMode = enabled
}

// GetMemoryStats returns statistics about conversation memory
func (ae *AgentExecutor) GetMemoryStats() map[string]interface{} {
	ae.memoryMutex.RLock()
	defer ae.memoryMutex.RUnlock()

	totalExecutions := 0
	totalSuccess := 0
	activeTasks := 0
	securityDistribution := make(map[string]int)
	modelDistribution := make(map[string]int)

	for _, memory := range ae.conversationMemory {
		totalExecutions += memory.ExecutionCount
		totalSuccess += memory.SuccessCount
		securityDistribution[memory.SecurityLevel]++
		modelDistribution[memory.ModelPreference]++
		if time.Since(memory.LastUpdated) < 24*time.Hour {
			activeTasks++
		}
	}

	successRate := 0.0
	if totalExecutions > 0 {
		successRate = float64(totalSuccess) / float64(totalExecutions) * 100
	}

	return map[string]interface{}{
		"total_tasks":           len(ae.conversationMemory),
		"active_tasks":          activeTasks,
		"total_executions":      totalExecutions,
		"successful_executions": totalSuccess,
		"success_rate":          successRate,
		"learning_enabled":      ae.learningEnabled,
		"self_reflection":       ae.selfReflection,
		"autonomous_mode":       ae.autonomousMode,
		"security_distribution": securityDistribution,
		"model_distribution":    modelDistribution,
	}
}

// Utility functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
