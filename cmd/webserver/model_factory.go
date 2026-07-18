package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	conversationstenography "conversationstenography"
)

// ModelBackend selects the model inference backend based on environment.
type ModelBackend int

const (
	BackendLocal     ModelBackend = iota // Python subprocess (transformers/mlx)
	BackendOpenRouter                    // OpenRouter API
)

// ModelFactory creates and configures the appropriate model backend.
type ModelFactory struct {
	backend ModelBackend
}

// NewModelFactory reads the environment to decide which backend to use.
// OPENROUTER_ENABLED=true → BackendOpenRouter
// Otherwise → BackendLocal
func NewModelFactory() (*ModelFactory, error) {
	backend := BackendLocal
	if strings.EqualFold(os.Getenv("OPENROUTER_ENABLED"), "true") {
		backend = BackendOpenRouter
	}
	return &ModelFactory{backend: backend}, nil
}

// CreateModel creates and returns a LanguageModel instance based on the configured backend.
func (mf *ModelFactory) CreateModel(ctx context.Context) (conversationstenography.LanguageModel, error) {
	switch mf.backend {
	case BackendOpenRouter:
		return mf.createOpenRouterModel(ctx)
	case BackendLocal:
		return mf.createLocalModel(ctx)
	default:
		return nil, fmt.Errorf("unknown model backend: %d", mf.backend)
	}
}

func (mf *ModelFactory) createOpenRouterModel(ctx context.Context) (conversationstenography.LanguageModel, error) {
	cfg := conversationstenography.OpenRouterConfigFromEnv()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is required when OPENROUTER_ENABLED=true")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("OPENROUTER_MODEL is required when OPENROUTER_ENABLED=true")
	}

	// Start the lightweight tokenizer process (no model, just tokenizer)
	tokenizer, err := conversationstenography.NewPythonTokenizer(
		ctx,
		getPython(),
		cfg.Model,
		os.Getenv("OPENROUTER_TOKENIZER_REVISION"),
	)
	if err != nil {
		return nil, fmt.Errorf("start tokenizer: %w", err)
	}

	model := conversationstenography.NewOpenRouterModel(cfg, tokenizer)
	return model, nil
}

func (mf *ModelFactory) createLocalModel(ctx context.Context) (conversationstenography.LanguageModel, error) {
	modelName := os.Getenv("LOCAL_MODEL")
	if modelName == "" {
		modelName = "openai-community/gpt2"
	}
	python := getPython()

	// Start the full model process (original ProcessModel with hf_model.py)
	model, err := conversationstenography.NewProcessModel(
		ctx,
		python, "-u", "-m", "python.hf_model",
		"--model", modelName,
		"--device", getEnvOrDefault("LOCAL_DEVICE", "cpu"),
	)
	if err != nil {
		return nil, fmt.Errorf("start local model: %w", err)
	}

	return model, nil
}

// BackendName returns a human-readable name of the active backend.
func (mf *ModelFactory) BackendName() string {
	switch mf.backend {
	case BackendOpenRouter:
		return "openrouter"
	case BackendLocal:
		return "local"
	default:
		return "unknown"
	}
}

func getPython() string {
	if p := os.Getenv("CONVERSATION_STENOGRAPHY_PYTHON"); p != "" {
		return p
	}
	return "python3"
}


