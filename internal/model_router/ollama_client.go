// internal/model_router/ollama_client.go
package model_router

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

type OllamaClient struct {
    baseURL    string
    httpClient *http.Client
}

type OllamaModel struct {
    Name         string            `json:"name"`
    ModifiedAt   time.Time         `json:"modified_at"`
    Size         int64             `json:"size"`
    Digest       string            `json:"digest"`
    Details      OllamaModelDetails `json:"details"`
}

type OllamaModelDetails struct {
    Format            string   `json:"format"`
    Family            string   `json:"family"`
    Families          []string `json:"families"`
    ParameterSize     string   `json:"parameter_size"`
    QuantizationLevel string   `json:"quantization_level"`
}

type OllamaModelsResponse struct {
    Models []OllamaModel `json:"models"`
}

type OllamaChatRequest struct {
    Model    string                 `json:"model"`
    Messages []OllamaChatMessage    `json:"messages"`
    Stream   bool                   `json:"stream"`
    Options  map[string]interface{} `json:"options,omitempty"`
}

type OllamaChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type OllamaChatResponse struct {
    Model     string            `json:"model"`
    CreatedAt time.Time         `json:"created_at"`
    Message   OllamaChatMessage `json:"message"`
    Done      bool              `json:"done"`
}

func NewOllamaClient(baseURL string) *OllamaClient {
    if baseURL == "" {
        baseURL = "http://localhost:11434"
    }
    return &OllamaClient{
        baseURL:    baseURL,
        httpClient: &http.Client{Timeout: 60 * time.Second},
    }
}

func (c *OllamaClient) ListModels(ctx context.Context) ([]OllamaModel, error) {
    url := c.baseURL + "/api/tags"
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("ollama API returned status %d", resp.StatusCode)
    }
    
    var modelsResp OllamaModelsResponse
    if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
        return nil, err
    }
    
    return modelsResp.Models, nil
}

func (c *OllamaClient) Chat(ctx context.Context, req OllamaChatRequest) (*OllamaChatResponse, error) {
    url := c.baseURL + "/api/chat"
    
    reqBody, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }
    
    httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("ollama chat API returned status %d", resp.StatusCode)
    }
    
    var chatResp OllamaChatResponse
    if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
        return nil, err
    }
    
    return &chatResp, nil
}
