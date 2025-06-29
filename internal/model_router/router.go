// internal/model_router/router.go
package model_router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"mcp-cron-persistent/internal/model"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type CompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type ModelRouter struct {
	profiles      map[string]*ModelProfile
	gatewayClient *GatewayClient
	preferences   *UserPreferences
	usageTracker  *UsageTracker
	enabled       bool
}

type ModelProfile struct {
	Name          string        `json:"name"`
	Provider      string        `json:"provider"`
	Cost          int           `json:"cost"`        // 1-10 scale
	Speed         int           `json:"speed"`       // 1-10 scale
	Capability    int           `json:"capability"`  // 1-10 scale
	Security      SecurityLevel `json:"security"`    // local, private, public
	Specialties   []string      `json:"specialties"` // coding, analysis, creative, math
	MaxTokens     int           `json:"maxTokens"`
	InputCost     float64       `json:"inputCost"`  // per 1K tokens
	OutputCost    float64       `json:"outputCost"` // per 1K tokens
	ContextLength int           `json:"contextLength"`
	Features      []string      `json:"features"` // tool_use, vision, etc.
}

type SecurityLevel string

const (
	SecurityLocal   SecurityLevel = "local"
	SecurityPrivate SecurityLevel = "private"
	SecurityPublic  SecurityLevel = "public"
)

type UserPreferences struct {
	MaxCostPerExecution float64       `json:"maxCostPerExecution"`
	PreferredSecurity   SecurityLevel `json:"preferredSecurity"`
	PreferredProviders  []string      `json:"preferredProviders"`
	AvoidProviders      []string      `json:"avoidProviders"`
	SpeedWeight         float64       `json:"speedWeight"`
	CostWeight          float64       `json:"costWeight"`
	CapabilityWeight    float64       `json:"capabilityWeight"`
	SecurityWeight      float64       `json:"securityWeight"`
}

type TaskContext struct {
	Task             *model.Task   `json:"task"`
	Hint             string        `json:"hint,omitempty"`
	RequiredFeatures []string      `json:"requiredFeatures,omitempty"`
	MaxCost          float64       `json:"maxCost,omitempty"`
	MaxLatency       time.Duration `json:"maxLatency,omitempty"`
	SecurityLevel    SecurityLevel `json:"securityLevel,omitempty"`
	Priority         string        `json:"priority,omitempty"`
}

type ModelSelection struct {
	Model         *ModelProfile   `json:"model"`
	Score         float64         `json:"score"`
	Reasoning     string          `json:"reasoning"`
	EstimatedCost float64         `json:"estimatedCost"`
	EstimatedTime time.Duration   `json:"estimatedTime"`
	Alternatives  []*ModelProfile `json:"alternatives,omitempty"`
}

type UsageTracker struct {
	usage map[string]*ModelUsage
}

type ModelUsage struct {
	TotalCalls  int           `json:"totalCalls"`
	TotalTokens int           `json:"totalTokens"`
	TotalCost   float64       `json:"totalCost"`
	AvgLatency  time.Duration `json:"avgLatency"`
	SuccessRate float64       `json:"successRate"`
	LastUsed    time.Time     `json:"lastUsed"`
}

type GatewayClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewModelRouter(gatewayURL string) *ModelRouter {
	client := &GatewayClient{
		baseURL:    strings.TrimSuffix(gatewayURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	router := &ModelRouter{
		profiles:      make(map[string]*ModelProfile),
		gatewayClient: client,
		preferences:   NewDefaultUserPreferences(),
		usageTracker:  NewUsageTracker(),
		enabled:       true,
	}

	// Load default model profiles
	router.loadDefaultProfiles()

	return router
}

func NewDefaultUserPreferences() *UserPreferences {
	return &UserPreferences{
		MaxCostPerExecution: 0.50, // 50 cents max per execution
		PreferredSecurity:   SecurityPrivate,
		SpeedWeight:         0.3,
		CostWeight:          0.4,
		CapabilityWeight:    0.2,
		SecurityWeight:      0.1,
	}
}

func NewUsageTracker() *UsageTracker {
	return &UsageTracker{
		usage: make(map[string]*ModelUsage),
	}
}

func (mr *ModelRouter) loadDefaultProfiles() {
	profiles := []*ModelProfile{
		{
			Name:          "anthropic/claude-3.5-sonnet",
			Provider:      "anthropic",
			Cost:          8,
			Speed:         7,
			Capability:    10,
			Security:      SecurityPublic,
			Specialties:   []string{"analysis", "coding", "reasoning"},
			MaxTokens:     8192,
			InputCost:     0.003,
			OutputCost:    0.015,
			ContextLength: 200000,
			Features:      []string{"tool_use", "vision"},
		},
		{
			Name:          "openai/gpt-4o-mini",
			Provider:      "openai",
			Cost:          3,
			Speed:         9,
			Capability:    7,
			Security:      SecurityPublic,
			Specialties:   []string{"general", "fast"},
			MaxTokens:     4096,
			InputCost:     0.00015,
			OutputCost:    0.0006,
			ContextLength: 128000,
			Features:      []string{"tool_use", "vision"},
		},
		{
			Name:          "meta-llama/llama-3.1-70b-instruct",
			Provider:      "meta",
			Cost:          5,
			Speed:         6,
			Capability:    8,
			Security:      SecurityPublic,
			Specialties:   []string{"coding", "math", "reasoning"},
			MaxTokens:     4096,
			InputCost:     0.00088,
			OutputCost:    0.00088,
			ContextLength: 131072,
			Features:      []string{"tool_use"},
		},
		{
			Name:          "local/ollama",
			Provider:      "local",
			Cost:          1,
			Speed:         4,
			Capability:    6,
			Security:      SecurityLocal,
			Specialties:   []string{"privacy", "local"},
			MaxTokens:     4096,
			InputCost:     0.0,
			OutputCost:    0.0,
			ContextLength: 32768,
			Features:      []string{},
		},
	}

	for _, profile := range profiles {
		mr.profiles[profile.Name] = profile
	}
}

func (mr *ModelRouter) SelectModel(ctx TaskContext) (*ModelSelection, error) {
	if !mr.enabled {
		return &ModelSelection{
			Model:     mr.profiles["anthropic/claude-3.5-sonnet"],
			Score:     1.0,
			Reasoning: "Model router disabled, using default",
		}, nil
	}

	requirements := mr.analyzeTaskRequirements(ctx)
	candidates := mr.filterCandidates(requirements, ctx)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models match the requirements")
	}

	scored := mr.scoreModels(candidates, requirements, ctx)
	best := scored[0]

	alternatives := make([]*ModelProfile, 0, min(3, len(scored)-1))
	for i := 1; i < len(scored) && i <= 3; i++ {
		alternatives = append(alternatives, scored[i].ModelProfile) // Access embedded field
	}

	return &ModelSelection{
		Model:         best.ModelProfile, // Access embedded field
		Score:         best.Score,
		Reasoning:     best.Reasoning,
		EstimatedCost: best.EstimatedCost,
		EstimatedTime: best.EstimatedTime,
		Alternatives:  alternatives,
	}, nil
}

type TaskRequirements struct {
	RequiresTools   bool
	RequiresVision  bool
	EstimatedTokens int
	ComplexityLevel int // 1-10
	SecurityLevel   SecurityLevel
	SpeedImportance int // 1-10
	CostImportance  int // 1-10
}

func (mr *ModelRouter) analyzeTaskRequirements(ctx TaskContext) *TaskRequirements {
	req := &TaskRequirements{
		SecurityLevel:   mr.preferences.PreferredSecurity,
		SpeedImportance: 5,
		CostImportance:  5,
	}

	// Override with context security if specified
	if ctx.SecurityLevel != "" {
		req.SecurityLevel = ctx.SecurityLevel
	}

	// Analyze task content for requirements
	if ctx.Task != nil {
		content := strings.ToLower(ctx.Task.Prompt + " " + ctx.Task.Description)

		// Check for tool requirements
		if strings.Contains(content, "tool") || strings.Contains(content, "function") ||
			strings.Contains(content, "call") || strings.Contains(content, "execute") {
			req.RequiresTools = true
		}

		// Check for vision requirements
		if strings.Contains(content, "image") || strings.Contains(content, "vision") ||
			strings.Contains(content, "picture") || strings.Contains(content, "visual") {
			req.RequiresVision = true
		}

		// Estimate complexity
		req.ComplexityLevel = mr.estimateComplexity(content)

		// Estimate tokens (rough approximation)
		req.EstimatedTokens = len(strings.Fields(ctx.Task.Prompt)) * 4 / 3
	}

	// Apply context hint
	if ctx.Hint != "" {
		req = mr.applyHint(req, ctx.Hint)
	}

	return req
}

func (mr *ModelRouter) estimateComplexity(content string) int {
	complexity := 3 // baseline

	complexWords := []string{
		"analyze", "complex", "detailed", "comprehensive", "advanced",
		"algorithm", "optimization", "machine learning", "deep",
		"statistical", "mathematical", "research", "academic",
	}

	for _, word := range complexWords {
		if strings.Contains(content, word) {
			complexity++
		}
	}

	// Length also indicates complexity
	wordCount := len(strings.Fields(content))
	if wordCount > 100 {
		complexity += 2
	} else if wordCount > 50 {
		complexity += 1
	}

	if complexity > 10 {
		complexity = 10
	}

	return complexity
}

func (mr *ModelRouter) applyHint(req *TaskRequirements, hint string) *TaskRequirements {
	hint = strings.ToLower(hint)

	switch hint {
	case "fast", "quick", "speed":
		req.SpeedImportance = 9
		req.CostImportance = 3
	case "cheap", "cost", "economical":
		req.SpeedImportance = 3
		req.CostImportance = 9
	case "powerful", "capable", "advanced":
		req.ComplexityLevel = 10
		req.SpeedImportance = 3
		req.CostImportance = 3
	case "local", "private", "secure":
		req.SecurityLevel = SecurityLocal
	case "balanced":
		req.SpeedImportance = 5
		req.CostImportance = 5
	}

	return req
}

func (mr *ModelRouter) filterCandidates(req *TaskRequirements, ctx TaskContext) []*ModelProfile {
	var candidates []*ModelProfile

	for _, profile := range mr.profiles {
		// Check security requirements
		if req.SecurityLevel == SecurityLocal && profile.Security != SecurityLocal {
			continue
		}

		// Check feature requirements
		if req.RequiresTools && !mr.hasFeature(profile, "tool_use") {
			continue
		}

		if req.RequiresVision && !mr.hasFeature(profile, "vision") {
			continue
		}

		// Check provider preferences
		if mr.isProviderBlacklisted(profile.Provider) {
			continue
		}

		// Check cost limits
		if ctx.MaxCost > 0 {
			estimatedCost := mr.estimateCost(profile, req.EstimatedTokens)
			if estimatedCost > ctx.MaxCost {
				continue
			}
		}

		candidates = append(candidates, profile)
	}

	return candidates
}

func (mr *ModelRouter) hasFeature(profile *ModelProfile, feature string) bool {
	for _, f := range profile.Features {
		if f == feature {
			return true
		}
	}
	return false
}

func (mr *ModelRouter) isProviderBlacklisted(provider string) bool {
	for _, avoid := range mr.preferences.AvoidProviders {
		if avoid == provider {
			return true
		}
	}
	return false
}

func (mr *ModelRouter) estimateCost(profile *ModelProfile, tokens int) float64 {
	if tokens == 0 {
		tokens = 1000 // default estimate
	}

	inputCost := (float64(tokens) / 1000.0) * profile.InputCost
	outputCost := (float64(tokens) / 2.0 / 1000.0) * profile.OutputCost // assume output is half of input

	return inputCost + outputCost
}

type ScoredModel struct {
	*ModelProfile
	Score         float64
	Reasoning     string
	EstimatedCost float64
	EstimatedTime time.Duration
}

func (mr *ModelRouter) scoreModels(candidates []*ModelProfile, req *TaskRequirements, ctx TaskContext) []*ScoredModel {
	scored := make([]*ScoredModel, 0, len(candidates))

	for _, candidate := range candidates {
		score := mr.calculateScore(candidate, req, ctx)
		reasoning := mr.generateReasoning(candidate, req, score)
		cost := mr.estimateCost(candidate, req.EstimatedTokens)
		latency := mr.estimateLatency(candidate)

		scored = append(scored, &ScoredModel{
			ModelProfile:  candidate, // Now this works correctly
			Score:         score,
			Reasoning:     reasoning,
			EstimatedCost: cost,
			EstimatedTime: latency,
		})
	}

	// Sort by score (highest first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

func (mr *ModelRouter) calculateScore(profile *ModelProfile, req *TaskRequirements, _ TaskContext) float64 {
	var score float64

	// Base capability score
	capabilityScore := float64(profile.Capability) / 10.0
	if req.ComplexityLevel > 7 {
		capabilityScore *= 1.2 // Boost capability for complex tasks
	}

	// Speed score
	speedScore := float64(profile.Speed) / 10.0
	if req.SpeedImportance > 7 {
		speedScore *= 1.3
	}

	// Cost score (inverted - lower cost is better)
	costScore := (11 - float64(profile.Cost)) / 10.0
	if req.CostImportance > 7 {
		costScore *= 1.3
	}

	// Security score
	securityScore := 1.0
	if req.SecurityLevel == SecurityLocal && profile.Security == SecurityLocal {
		securityScore = 1.5
	} else if req.SecurityLevel == SecurityPrivate && profile.Security == SecurityPrivate {
		securityScore = 1.2
	}

	// Specialty match bonus
	specialtyBonus := 0.0
	if req.RequiresTools && mr.hasSpecialty(profile, "coding") {
		specialtyBonus += 0.1
	}

	// Apply weights from preferences
	score = (capabilityScore*mr.preferences.CapabilityWeight +
		speedScore*mr.preferences.SpeedWeight +
		costScore*mr.preferences.CostWeight +
		securityScore*mr.preferences.SecurityWeight) + specialtyBonus

	// Usage-based adjustment
	if usage, exists := mr.usageTracker.usage[profile.Name]; exists {
		if usage.SuccessRate < 0.8 {
			score *= 0.9 // Penalize unreliable models
		}
		if time.Since(usage.LastUsed) < time.Hour {
			score *= 1.05 // Slight bonus for recently used models (they're "warmed up")
		}
	}

	return score
}

func (mr *ModelRouter) hasSpecialty(profile *ModelProfile, specialty string) bool {
	for _, s := range profile.Specialties {
		if s == specialty {
			return true
		}
	}
	return false
}

func (mr *ModelRouter) generateReasoning(profile *ModelProfile, _ *TaskRequirements, score float64) string {
	reasons := make([]string, 0)

	if profile.Capability >= 8 {
		reasons = append(reasons, "high capability")
	}

	if profile.Speed >= 8 {
		reasons = append(reasons, "fast execution")
	}

	if profile.Cost <= 3 {
		reasons = append(reasons, "very cost-effective")
	}

	if profile.Security == SecurityLocal {
		reasons = append(reasons, "local/private execution")
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "well-balanced option")
	}

	return fmt.Sprintf("Selected for: %s (score: %.2f)", strings.Join(reasons, ", "), score)
}

func (mr *ModelRouter) estimateLatency(profile *ModelProfile) time.Duration {
	// Basic estimation based on model speed rating
	baseLatency := 5 * time.Second
	speedFactor := float64(11-profile.Speed) / 10.0

	return time.Duration(float64(baseLatency) * speedFactor)
}

func (mr *ModelRouter) RecordUsage(modelName string, tokens int, cost float64, latency time.Duration, success bool) {
	usage, exists := mr.usageTracker.usage[modelName]
	if !exists {
		usage = &ModelUsage{}
		mr.usageTracker.usage[modelName] = usage
	}

	usage.TotalCalls++
	usage.TotalTokens += tokens
	usage.TotalCost += cost
	usage.LastUsed = time.Now()

	// Update average latency
	usage.AvgLatency = time.Duration((int64(usage.AvgLatency)*int64(usage.TotalCalls-1) + int64(latency)) / int64(usage.TotalCalls))

	// Update success rate
	if success {
		usage.SuccessRate = (usage.SuccessRate*float64(usage.TotalCalls-1) + 1.0) / float64(usage.TotalCalls)
	} else {
		usage.SuccessRate = (usage.SuccessRate * float64(usage.TotalCalls-1)) / float64(usage.TotalCalls)
	}
}

func (mr *ModelRouter) CreateCompletion(ctx context.Context, taskCtx TaskContext, messages []Message) (*CompletionResponse, error) {
	selection, err := mr.SelectModel(taskCtx)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()

	req := &CompletionRequest{
		Model:       selection.Model.Name,
		Messages:    messages,
		MaxTokens:   selection.Model.MaxTokens,
		Temperature: 0.7,
	}

	resp, err := mr.gatewayClient.CreateCompletion(ctx, req)
	latency := time.Since(startTime)

	tokens := 0 // Would be extracted from response
	cost := selection.EstimatedCost
	success := err == nil
	mr.RecordUsage(selection.Model.Name, tokens, cost, latency, success)

	return resp, err
}

func (gc *GatewayClient) CreateCompletion(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "create_completion",
			"arguments": req,
		},
	}

	reqBody, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", gc.baseURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := gc.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var mcpResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mcpResp); err != nil {
		return nil, err
	}

	if mcpError, exists := mcpResp["error"]; exists {
		return nil, fmt.Errorf("MCP error: %v", mcpError)
	}

	result, exists := mcpResp["result"]
	if !exists {
		return nil, fmt.Errorf("no result in MCP response")
	}

	resultMap := result.(map[string]interface{})
	content := resultMap["content"].([]interface{})[0].(map[string]interface{})
	text := content["text"].(string)

	var compResp CompletionResponse
	if err := json.Unmarshal([]byte(text), &compResp); err != nil {
		return nil, err
	}

	return &compResp, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (mr *ModelRouter) LoadOllamaModels(ctx context.Context, ollamaURL string) error {
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	client := &OllamaClient{
		baseURL:    ollamaURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	models, err := client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to load OLLAMA models: %w", err)
	}

	for _, model := range models {
		profile := &ModelProfile{
			Name:          "local/" + model.Name,
			Provider:      "ollama",
			Cost:          1, // Local models are essentially free
			Speed:         mr.estimateOllamaSpeed(model),
			Capability:    mr.estimateOllamaCapability(model),
			Security:      SecurityLocal,
			Specialties:   mr.determineOllamaSpecialties(model),
			MaxTokens:     mr.estimateOllamaMaxTokens(model),
			InputCost:     0.0,
			OutputCost:    0.0,
			ContextLength: mr.estimateOllamaContextLength(model),
			Features:      mr.determineOllamaFeatures(model),
		}

		mr.profiles[profile.Name] = profile
	}

	return nil
}

func (mr *ModelRouter) estimateOllamaSpeed(model OllamaModel) int {
	sizeGB := float64(model.Size) / (1024 * 1024 * 1024)

	switch {
	case sizeGB < 2:
		return 9
	case sizeGB < 8:
		return 7
	case sizeGB < 20:
		return 5
	default:
		return 3
	}
}

func (mr *ModelRouter) estimateOllamaCapability(model OllamaModel) int {
	name := strings.ToLower(model.Name)

	switch {
	case strings.Contains(name, "llama3") && strings.Contains(name, "70b"):
		return 9
	case strings.Contains(name, "llama3") && strings.Contains(name, "8b"):
		return 7
	case strings.Contains(name, "mixtral"):
		return 8
	case strings.Contains(name, "qwen"):
		return 7
	case strings.Contains(name, "phi"):
		return 6
	case strings.Contains(name, "gemma"):
		return 6
	case strings.Contains(name, "mistral"):
		return 7
	default:
		return 5
	}
}

func (mr *ModelRouter) determineOllamaSpecialties(model OllamaModel) []string {
	name := strings.ToLower(model.Name)
	specialties := []string{"local", "privacy"}

	if strings.Contains(name, "code") || strings.Contains(name, "coder") {
		specialties = append(specialties, "coding")
	}
	if strings.Contains(name, "math") {
		specialties = append(specialties, "math")
	}
	if strings.Contains(name, "instruct") {
		specialties = append(specialties, "reasoning")
	}
	if strings.Contains(name, "chat") {
		specialties = append(specialties, "general")
	}

	return specialties
}

func (mr *ModelRouter) estimateOllamaMaxTokens(model OllamaModel) int {
	name := strings.ToLower(model.Name)

	switch {
	case strings.Contains(name, "32k"):
		return 32000
	case strings.Contains(name, "16k"):
		return 16000
	case strings.Contains(name, "8k"):
		return 8000
	default:
		return 4096
	}
}

func (mr *ModelRouter) estimateOllamaContextLength(model OllamaModel) int {
	name := strings.ToLower(model.Name)

	switch {
	case strings.Contains(name, "llama3"):
		return 8192
	case strings.Contains(name, "mixtral"):
		return 32768
	case strings.Contains(name, "qwen"):
		return 8192
	case strings.Contains(name, "gemma"):
		return 8192
	default:
		return 4096
	}
}

func (mr *ModelRouter) determineOllamaFeatures(model OllamaModel) []string {
	name := strings.ToLower(model.Name)
	features := []string{}

	if strings.Contains(name, "llama3") || strings.Contains(name, "qwen") ||
		strings.Contains(name, "mistral") {
		features = append(features, "tool_use")
	}

	if strings.Contains(name, "vision") || strings.Contains(name, "llava") {
		features = append(features, "vision")
	}

	return features
}

func (mr *ModelRouter) SelectModelWithOllama(ctx TaskContext, ollamaURL string) (*ModelSelection, error) {
	if ollamaURL != "" {
		if err := mr.LoadOllamaModels(context.Background(), ollamaURL); err != nil {
			fmt.Printf("Warning: Failed to load OLLAMA models: %v\n", err)
		}
	}

	return mr.SelectModel(ctx)
}

func (mr *ModelRouter) CreateOllamaCompletion(ctx context.Context, taskCtx TaskContext, messages []OllamaChatMessage, ollamaURL string) (*OllamaChatResponse, error) {
	selection, err := mr.SelectModelWithOllama(taskCtx, ollamaURL)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(selection.Model.Name, "local/") {
		return nil, fmt.Errorf("selected model %s is not an OLLAMA model", selection.Model.Name)
	}

	modelName := strings.TrimPrefix(selection.Model.Name, "local/")

	startTime := time.Now()
	client := &OllamaClient{
		baseURL:    ollamaURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}

	req := OllamaChatRequest{
		Model:    modelName,
		Messages: messages,
		Stream:   false,
	}

	resp, err := client.Chat(ctx, req)
	latency := time.Since(startTime)

	tokens := len(resp.Message.Content) / 4
	success := err == nil
	mr.RecordUsage(selection.Model.Name, tokens, 0.0, latency, success)

	return resp, err
}
