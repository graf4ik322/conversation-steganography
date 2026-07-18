package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	conversationstenography "conversationstenography"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	port := getEnvOrDefault("PORT", "8080")
	slidingTTL := parseDurationOrDefault("SESSION_SLIDING_TTL", "15m")
	maxTTL := parseDurationOrDefault("SESSION_MAX_TTL", "12h")

	fmt.Printf("\xf0\x9f\x94\x92 Conversation Steganography Web Service\n")
	fmt.Printf("   Session TTL: %s (sliding) / %s (hard cap)\n", slidingTTL, maxTTL)

	factory, err := NewModelFactory()
	if err != nil {
		return fmt.Errorf("model factory: %w", err)
	}
	fmt.Printf("   Model backend: %s\n", factory.BackendName())

	ctx := context.Background()
	model, err := factory.CreateModel(ctx)
	if err != nil {
		fmt.Printf("   ⚠ Model init failed: %v\n", err)
		fmt.Printf("   → API will return errors until a model is available\n")
	} else {
		defer func() {
			if closer, ok := model.(interface{ Close() error }); ok {
				closer.Close()
			}
		}()
	}
	_ = model // may be nil; handlers check model availability

	var fingerprint string
	if model != nil {
		fingerprint = model.Fingerprint()
	}

	cfg := &conversationstenography.GenerativeConfig{
		Prompt:           getEnvOrDefault("PROMPT", "Continue this casual chat message naturally"),
		TopN:             parseIntOrDefault("TOP_N", 5),
		Temperature:      parseFloatOrDefault("TEMPERATURE", 0.7),
		FinishTokens:     parseIntOrDefault("FINISH_TOKENS", 10),
		CandidatePool:    parseIntOrDefault("CANDIDATE_POOL", 20),
		CarrierTrials:    parseIntOrDefault("CARRIER_TRIALS", 3),
		NaturalnessSlack: parseFloatOrDefault("NATURALNESS_SLACK", 0.3),
		ModelFingerprint: fingerprint,
	}

	sm := NewSessionManager(slidingTTL, maxTTL)
	h := NewHandler(sm, model, cfg)

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/v1/session/start", h.handleSessionStart)
	mux.HandleFunc("/api/v1/session/status", h.handleSessionStatus)
	mux.HandleFunc("/api/v1/message/encode", h.handleEncode)
	mux.HandleFunc("/api/v1/message/decode", h.handleDecode)
	mux.HandleFunc("/api/v1/session/revoke", h.handleRevoke)
	mux.HandleFunc("/api/v1/events", h.handleSSE)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Serve frontend static files (SPA)
	staticDir := getEnvOrDefault("STATIC_DIR", "frontend/dist")
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(staticDir))
		mux.Handle("/", fs)
		fmt.Printf("   Static files: %s\n", staticDir)
	} else {
		fmt.Printf("   Static files: not found (%s), API-only mode\n", staticDir)
	}

	handler := securityHeaders(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("   Listening on :%s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-quit
	fmt.Println("\nShutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

func parseDurationOrDefault(envName, fallback string) time.Duration {
	v := os.Getenv(envName)
	if v == "" {
		d, _ := time.ParseDuration(fallback)
		return d
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		d, _ = time.ParseDuration(fallback)
	}
	return d
}

func parseIntOrDefault(envName string, fallback int) int {
	v := os.Getenv(envName)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func parseFloatOrDefault(envName string, fallback float64) float64 {
	v := os.Getenv(envName)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
