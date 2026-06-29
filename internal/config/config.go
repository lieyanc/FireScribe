package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lieyan/firescribe/internal/updater"
)

const DefaultPath = "config.json"

//go:embed default_config.json
var defaultConfigTemplate []byte

type Config struct {
	Path                  string         `json:"-"`
	Addr                  string         `json:"addr"`
	DataDir               string         `json:"data_dir"`
	DatabasePath          string         `json:"database_path"`
	WebDir                string         `json:"web_dir"`
	PromptPath            string         `json:"prompt_path"`
	UseMockOCR            bool           `json:"use_mock_ocr"`
	RequestTimeoutSeconds int            `json:"request_timeout_seconds"`
	RequestTimeout        time.Duration  `json:"-"`
	OpenAI                OpenAIConfig   `json:"openai"`
	Update                updater.Config `json:"update"`
}

type OpenAIConfig struct {
	BaseURL       string  `json:"base_url"`
	APIKey        string  `json:"api_key"`
	Model         string  `json:"model"`
	PromptVersion string  `json:"prompt_version"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
}

func Load() (Config, error) {
	return LoadFile(DefaultPath)
}

func LoadFile(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}

	defaults, err := defaultConfig(path)
	if err != nil {
		return Config{}, err
	}
	cfg := defaults
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := writeConfig(path, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var existing map[string]json.RawMessage
	if err := json.Unmarshal(raw, &existing); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if existing == nil {
		return Config{}, fmt.Errorf("parse config %s: top-level JSON value must be an object", path)
	}

	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Path = path
	applyDynamicDefaults(&cfg, existing, defaults)
	normalize(&cfg, defaults)

	changed, err := completeConfigFile(path, existing, cfg)
	if err != nil {
		return Config{}, err
	}
	if changed {
		if err := writeRawConfig(path, existing); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func defaultConfig(path string) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigTemplate, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse embedded default config: %w", err)
	}
	cfg.Path = path
	cfg.RequestTimeout = time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	return cfg, nil
}

func applyDynamicDefaults(cfg *Config, existing map[string]json.RawMessage, defaults Config) {
	if _, ok := existing["database_path"]; !ok {
		cfg.DatabasePath = filepath.Join(cfg.DataDir, filepath.Base(defaults.DatabasePath))
	}
	if _, ok := existing["use_mock_ocr"]; !ok {
		cfg.UseMockOCR = strings.TrimSpace(cfg.OpenAI.APIKey) == "" || strings.TrimSpace(cfg.OpenAI.Model) == ""
	}
}

func normalize(cfg *Config, defaults Config) {
	cfg.OpenAI.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.OpenAI.BaseURL), "/")
	if cfg.RequestTimeoutSeconds <= 0 {
		cfg.RequestTimeoutSeconds = defaults.RequestTimeoutSeconds
	}
	cfg.RequestTimeout = time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	cfg.Update.Channel = strings.ToLower(strings.TrimSpace(cfg.Update.Channel))
	if cfg.Update.Channel == "" {
		cfg.Update.Channel = defaults.Update.Channel
	}
	if cfg.Update.Channel != defaults.Update.Channel {
		cfg.Update.Channel = "dev"
	}
	if cfg.Update.CheckInterval <= 0 {
		cfg.Update.CheckInterval = defaults.Update.CheckInterval
	}
	if strings.TrimSpace(cfg.Update.ProxyBaseURL) == "" {
		cfg.Update.ProxyBaseURL = defaults.Update.ProxyBaseURL
	}
	cfg.Update.ProxyBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Update.ProxyBaseURL), "/")
	if strings.TrimSpace(cfg.Update.Repo) == "" {
		cfg.Update.Repo = defaults.Update.Repo
	}
	cfg.Update.Repo = strings.TrimSpace(cfg.Update.Repo)
}

func completeConfigFile(path string, existing map[string]json.RawMessage, cfg Config) (bool, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return false, fmt.Errorf("marshal config defaults for %s: %w", path, err)
	}
	var completed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &completed); err != nil {
		return false, fmt.Errorf("prepare config defaults for %s: %w", path, err)
	}
	return mergeMissing(existing, completed), nil
}

func mergeMissing(dst, src map[string]json.RawMessage) bool {
	changed := false
	for key, value := range src {
		current, ok := dst[key]
		if !ok {
			dst[key] = value
			changed = true
			continue
		}

		var currentObject map[string]json.RawMessage
		var valueObject map[string]json.RawMessage
		if json.Unmarshal(current, &currentObject) == nil && json.Unmarshal(value, &valueObject) == nil && currentObject != nil && valueObject != nil {
			if mergeMissing(currentObject, valueObject) {
				raw, _ := json.Marshal(currentObject)
				dst[key] = raw
				changed = true
			}
		}
	}
	return changed
}

func writeConfig(path string, cfg Config) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config %s: %w", path, err)
	}
	return writeFile(path, append(raw, '\n'))
}

func writeRawConfig(path string, cfg map[string]json.RawMessage) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config %s: %w", path, err)
	}
	return writeFile(path, append(raw, '\n'))
}

func writeFile(path string, raw []byte) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("prepare config dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
