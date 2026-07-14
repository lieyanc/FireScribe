package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
)

func TestCredentialsAreStoredOutsideSQLite(t *testing.T) {
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
	profile, err := application.SaveRecognizerProfile(ctx, app.RecognizerProfile{
		Name: "secure-profile", Driver: recognizer.DriverOpenAICompatible,
		BaseURL: "https://api.example.com/v1", APIKey: "profile-secret", Model: "vision-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := application.SaveProviderAdapter(ctx, app.ProviderAdapter{
		Name: "secure-adapter", Engine: recognizer.EngineGenericHTTPJSON,
		Endpoint: "https://ocr.example.com/v1", Model: "ocr-model", AuthType: "bearer", Secret: "adapter-secret", IsEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var databaseProfileSecret, databaseAdapterSecret string
	if err := conn.QueryRow(`SELECT api_key FROM recognizer_profiles WHERE id = ?`, profile.ID).Scan(&databaseProfileSecret); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT secret FROM provider_adapters WHERE id = ?`, adapter.ID).Scan(&databaseAdapterSecret); err != nil {
		t.Fatal(err)
	}
	if databaseProfileSecret != "" || databaseAdapterSecret != "" {
		t.Fatalf("database contains credentials: profile=%q adapter=%q", databaseProfileSecret, databaseAdapterSecret)
	}
	vaultPath := filepath.Join(files.Root, "secrets.json")
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "profile-secret") || !strings.Contains(string(raw), "adapter-secret") {
		t.Fatalf("credential vault did not retain secrets: %s", raw)
	}
	info, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("vault mode = %o, want 600", info.Mode().Perm())
	}
	loadedProfile, err := application.Store.GetRecognizerProfile(ctx, profile.ID)
	if err != nil || loadedProfile.APIKey != "profile-secret" || !loadedProfile.APIKeySet {
		t.Fatalf("loaded profile = %+v, err = %v", loadedProfile, err)
	}
	loadedAdapter, err := application.Store.GetProviderAdapter(ctx, adapter.ID)
	if err != nil || loadedAdapter.Secret != "adapter-secret" || !loadedAdapter.SecretSet {
		t.Fatalf("loaded adapter = %+v, err = %v", loadedAdapter, err)
	}
}

func TestConfigureSecretFileMigratesLegacyDatabaseCredentials(t *testing.T) {
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
	if _, err := conn.Exec(`
		INSERT INTO recognizer_profiles(id, name, driver, base_url, api_key, model, params_json, is_default, created_at, updated_at)
		VALUES ('legacy-profile', 'legacy', 'openai-compatible', 'https://api.example.com/v1', 'legacy-profile-secret', 'model', '{}', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO provider_adapters(id, name, engine, endpoint, model, auth_type, secret, timeout_seconds, request_config_json, response_config_json, is_enabled, created_at, updated_at)
		VALUES ('legacy-adapter', 'legacy-adapter', 'generic-http-json', 'https://ocr.example.com/v1', 'model', 'bearer', 'legacy-adapter-secret', 120, '{}', '{}', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(conn)
	vault := filepath.Join(root, "secrets.json")
	if err := store.ConfigureSecretFile(ctx, vault); err != nil {
		t.Fatal(err)
	}
	profile, err := store.GetRecognizerProfile(ctx, "legacy-profile")
	if err != nil || profile.APIKey != "legacy-profile-secret" {
		t.Fatalf("migrated profile = %+v, err=%v", profile, err)
	}
	adapter, err := store.GetProviderAdapter(ctx, "legacy-adapter")
	if err != nil || adapter.Secret != "legacy-adapter-secret" {
		t.Fatalf("migrated adapter = %+v, err=%v", adapter, err)
	}
	var profileDB, adapterDB string
	_ = conn.QueryRow(`SELECT api_key FROM recognizer_profiles WHERE id = 'legacy-profile'`).Scan(&profileDB)
	_ = conn.QueryRow(`SELECT secret FROM provider_adapters WHERE id = 'legacy-adapter'`).Scan(&adapterDB)
	if profileDB != "" || adapterDB != "" {
		t.Fatalf("legacy database credentials were not cleared: %q %q", profileDB, adapterDB)
	}
}
