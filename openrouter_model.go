package conversationstenography

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenRouterConfig holds configuration for the OpenRouter API backend.
type OpenRouterConfig struct {
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	BaseURL     string  `json:"base_url"`
	TopLogprobs int     `json:"top_logprobs"`
	Seed        int     `json:"seed"`
	Temperature float64 `json:"temperature"`
}

// DefaultOpenRouterConfig returns sensible defaults.
func DefaultOpenRouterConfig() OpenRouterConfig {
	return OpenRouterConfig{
		BaseURL:     "https://openrouter.ai/api/v1",
		TopLogprobs: 20,
		Seed:        42,
		Temperature: 0.7,
	}
}

// OpenRouterConfigFromEnv reads configuration from environment variables.
func OpenRouterConfigFromEnv() OpenRouterConfig {
	cfg := DefaultOpenRouterConfig()
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("OPENROUTER_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("OPENROUTER_BASE_URL"); v != "" {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}
	return cfg
}

// OpenRouterModel implements LanguageModel via the OpenRouter (OpenAI-compatible) API.
type OpenRouterModel struct {
	config    OpenRouterConfig
	httpClient *http.Client
	tokenizer Tokenizer // local tokenizer for Tokenize/Detokenize
	seed      int
}

// Tokenizer is the interface for local tokenization (not the LLM).
type Tokenizer interface {
	Tokenize(ctx context.Context, text string) ([]int, error)
	Detokenize(ctx context.Context, ids []int) (string, error)
	TokensToIDs(ctx context.Context, tokens []string) ([]int, error)
	Fingerprint() string
}

// NewOpenRouterModel creates a new OpenRouter-backed model.
func NewOpenRouterModel(config OpenRouterConfig, tokenizer Tokenizer) *OpenRouterModel {
	if config.TopLogprobs < 2 {
		config.TopLogprobs = 2
	}
	return &OpenRouterModel{
		config:    config,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		tokenizer: tokenizer,
		seed:      config.Seed,
	}
}

func (m *OpenRouterModel) Fingerprint() string {
	h := hmac.New(sha256.New, []byte("openrouter-fingerprint-v1"))
	h.Write([]byte(m.config.Model))
	return fmt.Sprintf("openrouter-%x", h.Sum(nil)[:8])
}

func (m *OpenRouterModel) Tokenize(ctx context.Context, text string) ([]int, error) {
	return m.tokenizer.Tokenize(ctx, text)
}

func (m *OpenRouterModel) Detokenize(ctx context.Context, ids []int) (string, error) {
	return m.tokenizer.Detokenize(ctx, ids)
}

// openRouterRequest is the JSON payload sent to the OpenAI-compatible API.
type openRouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens"`
	Logprobs    bool                `json:"logprobs"`
	TopLogprobs int                 `json:"top_logprobs"`
	Temperature float64             `json:"temperature"`
	Seed        *int                `json:"seed,omitempty"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	Choices []openRouterChoice `json:"choices"`
	Error   *openRouterError   `json:"error,omitempty"`
}

type openRouterChoice struct {
	Logprobs *openRouterLogprobs `json:"logprobs"`
}

type openRouterLogprobs struct {
	Content []openRouterTokenLogprob `json:"content"`
}

type openRouterTokenLogprob struct {
	Token       string                  `json:"token"`
	Logprob     float64                 `json:"logprob"`
	TopLogprobs []openRouterTopLogprob  `json:"top_logprobs"`
}

type openRouterTopLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
}

type openRouterError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (m *OpenRouterModel) Next(ctx context.Context, tokens []int, n int) ([]TokenCandidate, error) {
	if n > m.config.TopLogprobs {
		n = m.config.TopLogprobs
	}

	// Convert token IDs back to text for the API call
	contextText, err := m.tokenizer.Detokenize(ctx, tokens)
	if err != nil {
		return nil, fmt.Errorf("openrouter: detokenize context: %w", err)
	}

	reqBody := openRouterRequest{
		Model: m.config.Model,
		Messages: []openRouterMessage{
			{Role: "user", Content: contextText},
		},
		MaxTokens:   1,
		Logprobs:    true,
		TopLogprobs: n,
		Temperature: m.config.Temperature,
		Seed:        &m.seed,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter: marshal request: %w", err)
	}

	apiURL := m.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openrouter: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.config.APIKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/graf4ik322/conversation-steganography")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openrouter: read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openrouter: API error (HTTP %d): %s", resp.StatusCode, string(respBytes))
	}

	var apiResp openRouterResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("openrouter: parse response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("openrouter: API error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("openrouter: no choices in response")
	}

	logprobs := apiResp.Choices[0].Logprobs
	if logprobs == nil || len(logprobs.Content) == 0 {
		return nil, fmt.Errorf("openrouter: no logprobs in response")
	}

	topLogprobs := logprobs.Content[0].TopLogprobs
	if len(topLogprobs) < n {
		n = len(topLogprobs)
	}

	if n < 2 {
		return nil, fmt.Errorf("openrouter: need at least 2 candidates, got %d", n)
	}

	// Batch-convert token strings to IDs using the tokenizer's vocabulary.
	// This is correct because convert_tokens_to_ids uses the internal vocab
	// dictionary which is the true reverse mapping (unlike Tokenize/tokenizer.encode).
	tokenStrings := make([]string, 0, n)
	for _, tl := range topLogprobs[:n] {
		tokenStrings = append(tokenStrings, tl.Token)
	}
	ids, err := m.tokenizer.TokensToIDs(ctx, tokenStrings)
	if err != nil {
		return nil, fmt.Errorf("openrouter: tokens_to_ids: %w", err)
	}

	candidates := make([]TokenCandidate, 0, n)
	for i, tl := range topLogprobs[:n] {
		if i >= len(ids) {
			break
		}
		if ids[i] < 0 { // token not in vocabulary (shouldn't happen, but be safe)
			continue
		}
		candidates = append(candidates, TokenCandidate{
			ID:      ids[i],
			LogProb: tl.Logprob,
			Text:    tl.Token,
		})
	}

	if len(candidates) < 2 {
		return nil, fmt.Errorf("openrouter: insufficient valid candidates (%d) after token mapping", len(candidates))
	}

	return candidates, nil
}

// Close is a no-op for OpenRouterModel (no local process to clean up).
func (m *OpenRouterModel) Close() error { return nil }
