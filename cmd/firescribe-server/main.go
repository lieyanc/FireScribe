package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lieyan/firescribe/internal/api"
	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/config"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func main() {
	cfg := config.Load()

	conn, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	if err := db.Migrate(conn); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	files, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("prepare storage: %v", err)
	}

	rec := buildRecognizer(cfg)
	application := app.New(app.NewStore(conn), files, rec)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(application, cfg.WebDir).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("FireScribe listening on http://localhost%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func buildRecognizer(cfg config.Config) recognizer.Recognizer {
	if cfg.UseMockOCR {
		log.Printf("OCR recognizer: mock (set FIRESCRIBE_USE_MOCK_OCR=false, FIRESCRIBE_OPENAI_MODEL and an API key to use OpenAI compatible OCR)")
		return recognizer.MockRecognizer{}
	}
	prompt, err := os.ReadFile(cfg.PromptPath)
	if err != nil {
		log.Printf("read prompt %s: %v", cfg.PromptPath, err)
	}
	log.Printf("OCR recognizer: OpenAI compatible model=%s base_url=%s", cfg.OpenAI.Model, cfg.OpenAI.BaseURL)
	return recognizer.NewOpenAI(recognizer.OpenAIConfig{
		BaseURL:       cfg.OpenAI.BaseURL,
		APIKey:        cfg.OpenAI.APIKey,
		Model:         cfg.OpenAI.Model,
		Prompt:        string(prompt),
		PromptVersion: cfg.OpenAI.PromptVersion,
		Temperature:   cfg.OpenAI.Temperature,
		MaxTokens:     cfg.OpenAI.MaxTokens,
		Timeout:       cfg.RequestTimeout,
	})
}
