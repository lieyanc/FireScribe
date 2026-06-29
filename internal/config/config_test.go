package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFileCreatesDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DatabasePath != filepath.Join("data", "firescribe.db") {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.RequestTimeout != 120*time.Second {
		t.Fatalf("RequestTimeout = %v, want 120s", cfg.RequestTimeout)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("persisted config is not JSON: %v", err)
	}
	if _, ok := persisted["openai"].(map[string]any); !ok {
		t.Fatalf("persisted config missing openai object: %v", persisted)
	}
	if _, ok := persisted["update"].(map[string]any); !ok {
		t.Fatalf("persisted config missing update object: %v", persisted)
	}
}

func TestLoadFileCompletesMissingTopLevelAndNestedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := []byte(`{
  "addr": ":9090",
  "data_dir": "custom-data",
  "openai": {
    "model": "vision-model"
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.DataDir != "custom-data" {
		t.Fatalf("DataDir = %q, want custom-data", cfg.DataDir)
	}
	if cfg.DatabasePath != filepath.Join("custom-data", "firescribe.db") {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if !cfg.UseMockOCR {
		t.Fatal("UseMockOCR = false, want true when API key is missing")
	}
	if cfg.OpenAI.Model != "vision-model" {
		t.Fatalf("OpenAI.Model = %q, want vision-model", cfg.OpenAI.Model)
	}
	if cfg.OpenAI.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("OpenAI.BaseURL = %q", cfg.OpenAI.BaseURL)
	}
	if cfg.Update.Channel != "stable" || cfg.Update.CheckInterval != 3600 || cfg.Update.Repo != "lieyanc/FireScribe" {
		t.Fatalf("Update = %#v", cfg.Update)
	}

	persisted := readConfigMap(t, path)
	if persisted["addr"] != ":9090" {
		t.Fatalf("persisted addr = %v", persisted["addr"])
	}
	if persisted["database_path"] != filepath.Join("custom-data", "firescribe.db") {
		t.Fatalf("persisted database_path = %v", persisted["database_path"])
	}
	openAI, ok := persisted["openai"].(map[string]any)
	if !ok {
		t.Fatalf("persisted openai = %T", persisted["openai"])
	}
	if openAI["model"] != "vision-model" {
		t.Fatalf("persisted openai.model = %v", openAI["model"])
	}
	if openAI["max_tokens"].(float64) != 4096 {
		t.Fatalf("persisted openai.max_tokens = %v", openAI["max_tokens"])
	}
	update, ok := persisted["update"].(map[string]any)
	if !ok {
		t.Fatalf("persisted update = %T", persisted["update"])
	}
	if update["repo"] != "lieyanc/FireScribe" {
		t.Fatalf("persisted update.repo = %v", update["repo"])
	}
}

func TestLoadFilePreservesExplicitValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := []byte(`{
  "data_dir": "custom-data",
  "database_path": "db/custom.db",
  "use_mock_ocr": false,
  "request_timeout_seconds": 30,
  "openai": {
    "base_url": "https://example.test/v1/",
    "api_key": "secret",
    "model": "vision-model",
    "prompt_version": "custom",
    "temperature": 0.25,
    "max_tokens": 1024
  },
  "update": {
    "enabled": true,
    "channel": "dev",
    "check_interval": 30,
    "proxy_base_url": "https://proxy.example/base/",
    "repo": "owner/repo"
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if cfg.DatabasePath != "db/custom.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.UseMockOCR {
		t.Fatal("UseMockOCR = true, want false")
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("RequestTimeout = %v, want 30s", cfg.RequestTimeout)
	}
	if cfg.OpenAI.BaseURL != "https://example.test/v1" {
		t.Fatalf("OpenAI.BaseURL = %q", cfg.OpenAI.BaseURL)
	}
	if !cfg.Update.Enabled || cfg.Update.Channel != "dev" || cfg.Update.CheckInterval != 30 ||
		cfg.Update.ProxyBaseURL != "https://proxy.example/base" || cfg.Update.Repo != "owner/repo" {
		t.Fatalf("Update = %#v", cfg.Update)
	}
}

func readConfigMap(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return parsed
}
