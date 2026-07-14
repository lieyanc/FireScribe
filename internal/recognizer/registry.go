package recognizer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DriverOpenAICompatible = "openai-compatible"
	DriverMock             = "mock"
	EngineGenericHTTPJSON  = "generic-http-json"
)

// ProfileConfig is the allow-listed, data-only configuration accepted by a
// recognizer driver. Profiles never name executables or load local code.
type ProfileConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	ParamsJSON    string
	PromptText    string
	PromptVersion string
}

type Factory func(ProfileConfig) (Recognizer, error)

// Registry maps a small allow-list of driver identifiers to in-process
// factories. It deliberately has no filesystem/plugin loading path.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	r := &Registry{factories: map[string]Factory{}}
	r.Register(DriverMock, func(cfg ProfileConfig) (Recognizer, error) {
		return WithPromptSnapshot(MockRecognizer{}, cfg.PromptText, cfg.PromptVersion), nil
	})
	r.Register(DriverOpenAICompatible, buildOpenAIProfile)
	return r
}

func (r *Registry) Register(driver string, factory Factory) {
	driver = strings.TrimSpace(driver)
	if driver == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[driver] = factory
}

func (r *Registry) Drivers() []string {
	return []string{DriverOpenAICompatible, DriverMock}
}

func (r *Registry) Build(driver string, cfg ProfileConfig) (Recognizer, error) {
	r.mu.RLock()
	factory := r.factories[strings.TrimSpace(driver)]
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("unsupported recognizer driver %q", driver)
	}
	return factory(cfg)
}

// BuildProviderAdapter constructs a recognizer from a persisted, data-only
// manifest. The engine identifier is allow-listed here; manifests can never
// point at executables, shared libraries, scripts, or local Go plugins.
func (r *Registry) BuildProviderAdapter(manifest ProviderManifest) (Recognizer, error) {
	switch strings.TrimSpace(manifest.Engine) {
	case EngineGenericHTTPJSON:
		return NewGenericHTTPJSON(manifest)
	default:
		return nil, fmt.Errorf("unsupported provider adapter engine %q", manifest.Engine)
	}
}

type openAIProfileParams struct {
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"max_tokens"`
	MaxImageEdge   int     `json:"max_image_edge"`
	RetryAttempts  int     `json:"retry_attempts"`
	TimeoutSeconds int     `json:"timeout_seconds"`
}

func buildOpenAIProfile(cfg ProfileConfig) (Recognizer, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base_url is required for openai-compatible profiles")
	}
	if !strings.HasPrefix(cfg.BaseURL, "http://") && !strings.HasPrefix(cfg.BaseURL, "https://") {
		return nil, fmt.Errorf("base_url must start with http:// or https://")
	}
	parsedBaseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("base_url must be a valid absolute URL")
	}
	if parsedBaseURL.User != nil || parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return nil, fmt.Errorf("base_url must not contain credentials, query parameters, or fragments")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("api_key is required for openai-compatible profiles")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("model is required for openai-compatible profiles")
	}
	params := openAIProfileParams{Temperature: 0, MaxTokens: 4096, RetryAttempts: 3, TimeoutSeconds: 120}
	if raw := strings.TrimSpace(cfg.ParamsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			return nil, fmt.Errorf("parse params_json: %w", err)
		}
	}
	if params.Temperature < 0 || params.Temperature > 2 {
		return nil, fmt.Errorf("temperature must be between 0 and 2")
	}
	if params.MaxTokens <= 0 {
		return nil, fmt.Errorf("max_tokens must be positive")
	}
	if params.MaxImageEdge < 0 || params.MaxImageEdge > 8192 {
		return nil, fmt.Errorf("max_image_edge must be between 0 and 8192")
	}
	if params.RetryAttempts < 1 || params.RetryAttempts > 10 {
		return nil, fmt.Errorf("retry_attempts must be between 1 and 10")
	}
	if params.TimeoutSeconds < 10 || params.TimeoutSeconds > 3600 {
		return nil, fmt.Errorf("timeout_seconds must be between 10 and 3600")
	}
	return NewOpenAI(OpenAIConfig{
		BaseURL:       strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		APIKey:        strings.TrimSpace(cfg.APIKey),
		Model:         strings.TrimSpace(cfg.Model),
		PromptText:    cfg.PromptText,
		PromptVersion: cfg.PromptVersion,
		Temperature:   params.Temperature,
		MaxTokens:     params.MaxTokens,
		MaxImageEdge:  params.MaxImageEdge,
		RetryAttempts: params.RetryAttempts,
		Timeout:       time.Duration(params.TimeoutSeconds) * time.Second,
	}), nil
}

type promptSnapshotter interface {
	WithPromptSnapshot(text, version string) Recognizer
}

type promptTextProvider interface {
	PromptSnapshotText() string
}

type retrySecretProvider interface {
	RetrySecret() string
}

func PromptSnapshotText(rec Recognizer) string {
	if provider, ok := rec.(promptTextProvider); ok {
		return provider.PromptSnapshotText()
	}
	return ""
}

// RetrySecret returns only the live credential held by an already configured
// in-process recognizer. It is used to combine a current secret with an old
// run's immutable non-secret configuration; it is never serialized.
func RetrySecret(rec Recognizer) string {
	if provider, ok := rec.(retrySecretProvider); ok {
		return provider.RetrySecret()
	}
	return ""
}

// WithPromptSnapshot applies a run-local prompt without mutating the globally
// active prompt. Drivers that do not consume prompts still receive an audit
// wrapper so the selected version is recorded on the run.
func WithPromptSnapshot(rec Recognizer, text, version string) Recognizer {
	if strings.TrimSpace(version) == "" {
		return rec
	}
	if configurable, ok := rec.(promptSnapshotter); ok {
		return configurable.WithPromptSnapshot(text, version)
	}
	return promptSnapshotRecognizer{Recognizer: rec, version: version, text: text}
}

type promptSnapshotRecognizer struct {
	Recognizer
	version string
	text    string
}

func (r promptSnapshotRecognizer) MergeCandidates(ctx context.Context, input CandidateMergeInput) (CandidateMergeResult, error) {
	merger, ok := r.Recognizer.(CandidateMerger)
	if !ok {
		return CandidateMergeResult{}, fmt.Errorf("recognizer driver %q does not support candidate merging", r.Provider())
	}
	return merger.MergeCandidates(ctx, input)
}

func (r promptSnapshotRecognizer) PromptVersion() string {
	return promptVersionWithHash(r.version, r.text)
}

func (r promptSnapshotRecognizer) ConfigJSON() string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(r.Recognizer.ConfigJSON()), &cfg); err != nil || cfg == nil {
		cfg = map[string]any{}
	}
	_, hash := hashPrompt(r.text)
	cfg["prompt_version"] = r.version
	cfg["prompt_hash"] = hash
	raw, _ := json.Marshal(cfg)
	return string(raw)
}
