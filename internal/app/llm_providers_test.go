package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestLLMProviderOwnsCredentialsAndHostsMultipleModels(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	conn, err := db.Open(filepath.Join(root, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})

	provider, err := application.SaveLLMProvider(ctx, app.LLMProvider{
		Name: "Gateway", Driver: recognizer.DriverOpenAICompatible,
		BaseURL: "https://api.example.com/v1", APIKey: "shared-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !provider.APIKeySet {
		t.Fatal("expected provider api_key_set")
	}

	modelA, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		ProviderID: provider.ID, Name: "Fast", Model: "vision-fast", IsDefault: true,
		ParamsJSON: `{"temperature":0,"max_tokens":1024,"max_image_edge":0,"retry_attempts":1,"timeout_seconds":60}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	modelB, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		ProviderID: provider.ID, Name: "Accurate", Model: "vision-accurate",
		ParamsJSON: `{"temperature":0,"max_tokens":4096,"max_image_edge":2048,"retry_attempts":3,"timeout_seconds":120}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if modelA.BaseURL != "https://api.example.com/v1" || modelA.APIKey != "shared-secret" {
		t.Fatalf("model A should inherit provider credentials: %+v", modelA)
	}
	if modelB.BaseURL != modelA.BaseURL || modelB.APIKey != modelA.APIKey {
		t.Fatalf("models under same provider should share credentials: A=%+v B=%+v", modelA, modelB)
	}
	if modelA.Model == modelB.Model {
		t.Fatal("expected distinct model ids")
	}

	var dbKey string
	if err := conn.QueryRow(`SELECT api_key FROM recognizer_profiles WHERE id = ?`, modelA.ID).Scan(&dbKey); err != nil {
		t.Fatal(err)
	}
	if dbKey != "" {
		t.Fatalf("model row should not store api_key, got %q", dbKey)
	}

	def, ok, err := application.Store.DefaultRecognizerProfile(ctx)
	if err != nil || !ok || def.ID != modelA.ID {
		t.Fatalf("default model = %+v ok=%v err=%v", def, ok, err)
	}
}

func TestSeedLLMProvidersFromLegacyConfig(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	conn, err := db.Open(filepath.Join(root, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	if err := application.SeedLLMProvidersFromConfig(ctx, false, "https://api.openai.com/v1", "seed-key", "gpt-vision", 0, 2048, 1024, 2); err != nil {
		t.Fatal(err)
	}
	providers, err := application.Store.ListLLMProviders(ctx)
	if err != nil || len(providers) != 1 {
		t.Fatalf("providers = %#v err=%v", providers, err)
	}
	models, err := application.Store.ListRecognizerProfiles(ctx)
	if err != nil || len(models) != 1 || !models[0].IsDefault || models[0].Model != "gpt-vision" {
		t.Fatalf("models = %#v err=%v", models, err)
	}
	// Second seed is a no-op.
	if err := application.SeedLLMProvidersFromConfig(ctx, false, "https://other", "other", "other", 0, 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	providers, err = application.Store.ListLLMProviders(ctx)
	if err != nil || len(providers) != 1 {
		t.Fatalf("providers after reseed = %#v err=%v", providers, err)
	}
}
