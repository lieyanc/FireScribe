package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/lieyan/firescribe/internal/recognizer"
)

type AlignedCandidateSegmentInput struct {
	SourceResultID string `json:"source_result_id"`
	SourceStart    int    `json:"source_start"`
	SourceEnd      int    `json:"source_end"`
	Text           string `json:"text"`
}

func (a *App) MergeAlignedCandidates(ctx context.Context, pageID string, inputs []AlignedCandidateSegmentInput) (CandidateMerge, error) {
	page, err := a.Store.GetPage(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	if len(inputs) == 0 || len(inputs) > 1000 {
		return CandidateMerge{}, errors.New("between one and 1000 aligned segments are required")
	}
	results, err := a.Store.ListRecognitionResults(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	byID := make(map[string]RecognitionResult, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}
	createdAt := now()
	mergeID := newID("merge")
	var output strings.Builder
	seen := map[string]bool{}
	sourceIDs := []string{}
	segments := make([]CandidateMergeSegment, 0, len(inputs))
	outputOffset := 0
	for ordinal, input := range inputs {
		result, ok := byID[input.SourceResultID]
		if !ok {
			return CandidateMerge{}, fmt.Errorf("recognition result %q does not belong to page", input.SourceResultID)
		}
		units := utf16.Encode([]rune(result.Text))
		if input.SourceStart < 0 || input.SourceEnd < input.SourceStart || input.SourceEnd > len(units) {
			return CandidateMerge{}, fmt.Errorf("segment %d has an invalid UTF-16 source range", ordinal+1)
		}
		sourceText := string(utf16.Decode(units[input.SourceStart:input.SourceEnd]))
		if sourceText != input.Text {
			return CandidateMerge{}, fmt.Errorf("segment %d text does not match its source range", ordinal+1)
		}
		if !seen[input.SourceResultID] {
			seen[input.SourceResultID] = true
			sourceIDs = append(sourceIDs, input.SourceResultID)
		}
		output.WriteString(input.Text)
		end := outputOffset + len(utf16.Encode([]rune(input.Text)))
		segments = append(segments, CandidateMergeSegment{ID: newID("mseg"), CandidateMergeID: mergeID,
			Ordinal: ordinal, SourceResultID: input.SourceResultID, SourceStart: input.SourceStart, SourceEnd: input.SourceEnd,
			OutputStart: outputOffset, OutputEnd: end, Text: input.Text})
		outputOffset = end
	}
	if strings.TrimSpace(output.String()) == "" {
		return CandidateMerge{}, errors.New("aligned candidate text must not be empty")
	}
	baseID, err := a.effectiveBaseVersionID(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	version := TextVersion{ID: newID("txt"), DocumentID: page.DocumentID, PageID: pageID, Kind: "candidate", BaseVersionID: baseID,
		Text: output.String(), Status: "draft", CreatedBy: "aligned-segment-merge", CreatedAt: createdAt}
	raw, _ := json.Marshal(map[string]any{"mode": "aligned-segment-selection", "segments": inputs})
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	merge := CandidateMerge{ID: mergeID, PageID: pageID, TextVersionID: version.ID, SourceResultIDs: sourceIDs,
		Driver: "manual-alignment", PromptVersion: "manual_segment_selection_v1", PromptHash: hash,
		RawResponse: string(raw), CreatedAt: createdAt, TextVersion: version, Segments: segments}
	if err := a.Store.CreateCandidateMerge(ctx, merge, version); err != nil {
		return CandidateMerge{}, err
	}
	_ = a.Store.RecomputeDocumentStatus(ctx, page.DocumentID)
	return merge, nil
}

func (a *App) SaveLLMProvider(ctx context.Context, provider LLMProvider) (LLMProvider, error) {
	provider.ID = strings.TrimSpace(provider.ID)
	provider.Name = strings.TrimSpace(provider.Name)
	provider.Driver = strings.TrimSpace(provider.Driver)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if provider.Name == "" {
		return LLMProvider{}, errors.New("provider name is required")
	}
	if len([]rune(provider.Name)) > 128 {
		return LLMProvider{}, errors.New("provider name must not exceed 128 characters")
	}
	if provider.Driver == "" {
		provider.Driver = recognizer.DriverOpenAICompatible
	}
	if provider.Driver != recognizer.DriverOpenAICompatible && provider.Driver != recognizer.DriverMock {
		return LLMProvider{}, fmt.Errorf("unsupported provider driver %q", provider.Driver)
	}
	if provider.Driver == recognizer.DriverOpenAICompatible {
		if provider.BaseURL == "" {
			return LLMProvider{}, errors.New("base_url is required for openai-compatible providers")
		}
		if !strings.HasPrefix(provider.BaseURL, "http://") && !strings.HasPrefix(provider.BaseURL, "https://") {
			return LLMProvider{}, errors.New("base_url must start with http:// or https://")
		}
	} else {
		provider.BaseURL = ""
		provider.APIKey = ""
	}
	if provider.ID == "" {
		provider.ID = newID("llmp")
	} else if current, err := a.Store.GetLLMProvider(ctx, provider.ID); err == nil {
		if strings.TrimSpace(provider.APIKey) == "" {
			provider.APIKey = current.APIKey
		}
		provider.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return LLMProvider{}, err
	}
	if provider.Driver == recognizer.DriverOpenAICompatible && strings.TrimSpace(provider.APIKey) == "" {
		return LLMProvider{}, errors.New("api_key is required for openai-compatible providers")
	}
	return a.Store.SaveLLMProvider(ctx, provider)
}

func (a *App) DeleteLLMProvider(ctx context.Context, id string) error {
	return a.Store.DeleteLLMProvider(ctx, id)
}

// SaveRecognizerProfile saves a model under an LLM provider. Credentials and
// base_url live on the provider; the model only stores model id + params.
func (a *App) SaveRecognizerProfile(ctx context.Context, profile RecognizerProfile) (RecognizerProfile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.ProviderID = strings.TrimSpace(profile.ProviderID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Model = strings.TrimSpace(profile.Model)
	profile.ParamsJSON = strings.TrimSpace(profile.ParamsJSON)
	profile.PromptVersionID = strings.TrimSpace(profile.PromptVersionID)

	// Backward-compatible path: flat create with base_url/api_key still works by
	// ensuring a provider exists first (tests and older clients).
	if profile.ProviderID == "" {
		provider, err := a.ensureProviderForLegacyProfile(ctx, profile)
		if err != nil {
			return RecognizerProfile{}, err
		}
		profile.ProviderID = provider.ID
		profile.Driver = provider.Driver
		profile.BaseURL = provider.BaseURL
		profile.APIKey = provider.APIKey
	}

	provider, err := a.Store.GetLLMProvider(ctx, profile.ProviderID)
	if err != nil {
		return RecognizerProfile{}, fmt.Errorf("load provider: %w", err)
	}
	profile.Driver = provider.Driver
	profile.BaseURL = provider.BaseURL
	profile.APIKey = provider.APIKey
	profile.APIKeySet = provider.APIKeySet
	profile.ProviderName = provider.Name

	if profile.Name == "" {
		return RecognizerProfile{}, errors.New("model name is required")
	}
	if len([]rune(profile.Name)) > 128 {
		return RecognizerProfile{}, errors.New("model name must not exceed 128 characters")
	}
	if profile.ID == "" {
		profile.ID = newID("recp")
	} else if current, err := a.Store.GetRecognizerProfile(ctx, profile.ID); err == nil {
		profile.CreatedAt = current.CreatedAt
		if profile.ProviderID == "" {
			profile.ProviderID = current.ProviderID
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RecognizerProfile{}, err
	}
	if profile.Driver == recognizer.DriverOpenAICompatible && profile.Model == "" {
		return RecognizerProfile{}, errors.New("model is required for openai-compatible providers")
	}
	if profile.ParamsJSON == "" {
		profile.ParamsJSON = `{"temperature":0,"max_tokens":4096,"max_image_edge":0,"retry_attempts":3,"timeout_seconds":120}`
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(profile.ParamsJSON), &params); err != nil {
		return RecognizerProfile{}, fmt.Errorf("params_json must be valid JSON: %w", err)
	}
	allowedParams := map[string]bool{"temperature": true, "max_tokens": true, "max_image_edge": true, "retry_attempts": true, "timeout_seconds": true}
	for key := range params {
		if !allowedParams[key] {
			return RecognizerProfile{}, fmt.Errorf("params_json contains unsupported field %q", key)
		}
	}
	var prompt PromptVersion
	if profile.PromptVersionID != "" {
		var err error
		prompt, err = a.Store.GetPromptVersion(ctx, profile.PromptVersionID)
		if err != nil {
			return RecognizerProfile{}, fmt.Errorf("load prompt version: %w", err)
		}
	}
	if _, err := a.registry.Build(profile.Driver, recognizer.ProfileConfig{
		BaseURL: profile.BaseURL, APIKey: profile.APIKey, Model: profile.Model, ParamsJSON: profile.ParamsJSON,
		PromptText: prompt.Content, PromptVersion: prompt.Version,
	}); err != nil {
		return RecognizerProfile{}, err
	}
	return a.Store.SaveRecognizerProfile(ctx, profile)
}

func (a *App) ensureProviderForLegacyProfile(ctx context.Context, profile RecognizerProfile) (LLMProvider, error) {
	driver := strings.TrimSpace(profile.Driver)
	if driver == "" {
		driver = recognizer.DriverOpenAICompatible
	}
	baseURL := strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
	apiKey := strings.TrimSpace(profile.APIKey)

	// Reuse an existing provider with the same interface credentials when possible.
	providers, err := a.Store.ListLLMProviders(ctx)
	if err != nil {
		return LLMProvider{}, err
	}
	for _, item := range providers {
		if item.Driver == driver && item.BaseURL == baseURL && (apiKey == "" || item.APIKey == apiKey || !item.APIKeySet) {
			if apiKey != "" && item.APIKey != apiKey {
				item.APIKey = apiKey
				return a.SaveLLMProvider(ctx, item)
			}
			return item, nil
		}
	}
	name := providerNameFromLegacy(profile.Name, driver, baseURL)
	return a.SaveLLMProvider(ctx, LLMProvider{
		Name: name, Driver: driver, BaseURL: baseURL, APIKey: apiKey,
	})
}

// SeedLLMProvidersFromConfig creates a default provider+model from legacy
// config.json openai settings when the database has no providers yet.
func (a *App) SeedLLMProvidersFromConfig(ctx context.Context, useMock bool, baseURL, apiKey, model string, temperature float64, maxTokens, maxImageEdge, retryAttempts int) error {
	if _, err := a.Store.MigrateProfilesToLLMProviders(ctx); err != nil {
		return fmt.Errorf("migrate profiles to providers: %w", err)
	}
	providers, err := a.Store.ListLLMProviders(ctx)
	if err != nil {
		return err
	}
	if len(providers) > 0 {
		return nil
	}
	models, err := a.Store.ListRecognizerProfiles(ctx)
	if err != nil {
		return err
	}
	if len(models) > 0 {
		return nil
	}

	driver := recognizer.DriverOpenAICompatible
	name := "默认接口"
	if useMock || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" {
		driver = recognizer.DriverMock
		name = "Mock"
		baseURL = ""
		apiKey = ""
		model = "mock"
	}
	provider, err := a.SaveLLMProvider(ctx, LLMProvider{
		Name: name, Driver: driver, BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), APIKey: strings.TrimSpace(apiKey),
	})
	if err != nil {
		return err
	}
	params, _ := json.Marshal(map[string]any{
		"temperature":     temperature,
		"max_tokens":      maxTokens,
		"max_image_edge":  maxImageEdge,
		"retry_attempts":  retryAttempts,
		"timeout_seconds": 120,
	})
	displayName := model
	if displayName == "" {
		displayName = "默认模型"
	}
	_, err = a.SaveRecognizerProfile(ctx, RecognizerProfile{
		ProviderID: provider.ID,
		Name:       displayName,
		Model:      model,
		ParamsJSON: string(params),
		IsDefault:  true,
	})
	return err
}

func (a *App) SaveProviderAdapter(ctx context.Context, adapter ProviderAdapter) (ProviderAdapter, error) {
	adapter.ID = strings.TrimSpace(adapter.ID)
	adapter.Name = strings.TrimSpace(adapter.Name)
	adapter.Engine = strings.TrimSpace(adapter.Engine)
	adapter.Endpoint = strings.TrimSpace(adapter.Endpoint)
	adapter.Model = strings.TrimSpace(adapter.Model)
	adapter.AuthType = strings.TrimSpace(adapter.AuthType)
	adapter.RequestConfigJSON = strings.TrimSpace(adapter.RequestConfigJSON)
	adapter.ResponseConfigJSON = strings.TrimSpace(adapter.ResponseConfigJSON)
	if adapter.ID == "" {
		adapter.ID = newID("adapter")
	} else if current, err := a.Store.GetProviderAdapter(ctx, adapter.ID); err == nil {
		if strings.TrimSpace(adapter.Secret) == "" {
			adapter.Secret = current.Secret
		}
		adapter.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ProviderAdapter{}, err
	}
	if adapter.AuthType == "" {
		adapter.AuthType = "none"
	}
	if adapter.AuthType == "none" {
		adapter.Secret = ""
	}
	if adapter.Name == "" {
		return ProviderAdapter{}, errors.New("provider adapter name is required")
	}
	if len([]rune(adapter.Name)) > 128 {
		return ProviderAdapter{}, errors.New("provider adapter name must not exceed 128 characters")
	}
	if adapter.Engine == "" {
		adapter.Engine = recognizer.EngineGenericHTTPJSON
	}
	if adapter.TimeoutSeconds == 0 {
		adapter.TimeoutSeconds = 120
	}
	if adapter.RequestConfigJSON == "" {
		adapter.RequestConfigJSON = `{}`
	}
	if adapter.ResponseConfigJSON == "" {
		adapter.ResponseConfigJSON = `{"text_path":"text","confidence_path":"confidence","metadata_path":"metadata"}`
	}
	if _, err := a.registry.BuildProviderAdapter(providerManifest(adapter, recognizer.DefaultPrompt, "validation")); err != nil {
		return ProviderAdapter{}, err
	}
	return a.Store.SaveProviderAdapter(ctx, adapter)
}

func providerManifest(adapter ProviderAdapter, promptText, promptVersion string) recognizer.ProviderManifest {
	return recognizer.ProviderManifest{
		ID: adapter.ID, Name: adapter.Name, Engine: adapter.Engine, Endpoint: adapter.Endpoint, Model: adapter.Model,
		AuthType: adapter.AuthType, Secret: adapter.Secret, TimeoutSeconds: adapter.TimeoutSeconds,
		RequestConfigJSON: adapter.RequestConfigJSON, ResponseConfigJSON: adapter.ResponseConfigJSON,
		PromptText: promptText, PromptVersion: promptVersion,
	}
}

func (a *App) recognizerForRun(ctx context.Context, documentID, requestedProfileID, requestedAdapterID, requestedPromptID string) (recognizer.Recognizer, RecognizerProfile, ProviderAdapter, PromptVersion, AuthorRecognitionContext, error) {
	if strings.TrimSpace(requestedProfileID) != "" && strings.TrimSpace(requestedAdapterID) != "" {
		return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, errors.New("recognizer_profile_id and provider_adapter_id are mutually exclusive")
	}
	if requestedAdapterID = strings.TrimSpace(requestedAdapterID); requestedAdapterID != "" {
		adapter, err := a.Store.GetProviderAdapter(ctx, requestedAdapterID)
		if err != nil {
			return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, err
		}
		if !adapter.IsEnabled {
			return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, errors.New("provider adapter is disabled")
		}
		prompt, err := a.resolveRunPrompt(ctx, requestedPromptID, "")
		if err != nil {
			return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, err
		}
		authorContext, err := a.Store.BuildAuthorRecognitionContext(ctx, documentID, "")
		if err != nil {
			return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, fmt.Errorf("build author recognition context: %w", err)
		}
		promptText := prompt.Content
		if strings.TrimSpace(promptText) == "" {
			promptText = recognizer.DefaultPrompt
		}
		if authorContext.PromptContext != "" {
			promptText = strings.TrimSpace(promptText) + "\n\n" + authorContext.PromptContext
		}
		rec, err := a.registry.BuildProviderAdapter(providerManifest(adapter, promptText, prompt.Version))
		return rec, RecognizerProfile{}, adapter, prompt, authorContext, err
	}
	profile, hasProfile, err := a.resolveRecognizerProfile(ctx, requestedProfileID)
	if err != nil {
		return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, err
	}
	prompt, err := a.resolveRunPrompt(ctx, requestedPromptID, profile.PromptVersionID)
	if err != nil {
		return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, err
	}
	authorContext, err := a.Store.BuildAuthorRecognitionContext(ctx, documentID, "")
	if err != nil {
		return nil, RecognizerProfile{}, ProviderAdapter{}, PromptVersion{}, AuthorRecognitionContext{}, fmt.Errorf("build author recognition context: %w", err)
	}
	promptText := prompt.Content
	if hasProfile {
		if strings.TrimSpace(promptText) == "" {
			promptText = recognizer.DefaultPrompt
		}
		if authorContext.PromptContext != "" {
			promptText = strings.TrimSpace(promptText) + "\n\n" + authorContext.PromptContext
		}
		rec, err := a.registry.Build(profile.Driver, recognizer.ProfileConfig{
			BaseURL: profile.BaseURL, APIKey: profile.APIKey, Model: profile.Model, ParamsJSON: profile.ParamsJSON,
			PromptText: promptText, PromptVersion: prompt.Version,
		})
		return rec, profile, ProviderAdapter{}, prompt, authorContext, err
	}
	rec := a.Recognizer()
	if strings.TrimSpace(promptText) == "" {
		promptText = recognizer.PromptSnapshotText(rec)
	}
	if authorContext.PromptContext != "" {
		promptText = strings.TrimSpace(promptText) + "\n\n" + authorContext.PromptContext
	}
	if prompt.ID != "" || authorContext.PromptContext != "" {
		version := prompt.Version
		if version == "" {
			version = strings.SplitN(rec.PromptVersion(), "#", 2)[0]
		}
		rec = recognizer.WithPromptSnapshot(rec, promptText, version)
	}
	profile = RecognizerProfile{Driver: rec.Provider(), Name: "legacy-settings", Model: rec.Model()}
	return rec, profile, ProviderAdapter{}, prompt, authorContext, nil
}

func (a *App) resolveRecognizerProfile(ctx context.Context, requestedID string) (RecognizerProfile, bool, error) {
	if requestedID = strings.TrimSpace(requestedID); requestedID != "" {
		profile, err := a.Store.GetRecognizerProfile(ctx, requestedID)
		return profile, err == nil, err
	}
	return a.Store.DefaultRecognizerProfile(ctx)
}

func (a *App) resolveRunPrompt(ctx context.Context, requestedID, profilePromptID string) (PromptVersion, error) {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		id = strings.TrimSpace(profilePromptID)
	}
	if id != "" {
		return a.Store.GetPromptVersion(ctx, id)
	}
	prompt, _, err := a.Store.ActivePromptVersion(ctx)
	return prompt, err
}

type recognizerRunSnapshot struct {
	ProfileID            string          `json:"profile_id"`
	ProfileName          string          `json:"profile_name"`
	Driver               string          `json:"driver"`
	BaseURL              string          `json:"base_url"`
	Model                string          `json:"model"`
	ParamsJSON           string          `json:"params_json"`
	Params               json.RawMessage `json:"params,omitempty"`
	APIKeyConfigured     bool            `json:"api_key_configured"`
	ProviderAdapterID    string          `json:"provider_adapter_id,omitempty"`
	ProviderAdapterName  string          `json:"provider_adapter_name,omitempty"`
	ProviderEngine       string          `json:"provider_engine,omitempty"`
	Endpoint             string          `json:"endpoint,omitempty"`
	AuthType             string          `json:"auth_type,omitempty"`
	TimeoutSeconds       int             `json:"timeout_seconds,omitempty"`
	RequestConfigJSON    string          `json:"request_config_json,omitempty"`
	ResponseConfigJSON   string          `json:"response_config_json,omitempty"`
	PromptVersionID      string          `json:"prompt_version_id"`
	PromptVersion        string          `json:"prompt_version"`
	PromptSHA256         string          `json:"prompt_sha256"`
	PromptText           string          `json:"prompt_text"`
	AuthorProfileID      string          `json:"author_profile_id,omitempty"`
	AuthorPromptContext  string          `json:"author_prompt_context,omitempty"`
	AuthorContext        json.RawMessage `json:"author_context,omitempty"`
	AuthorContextOmitted bool            `json:"author_context_omitted,omitempty"`
}

const (
	maxAuthorPromptContextRunes = 16_384
	maxAuthorContextSnapshot    = 64 << 10
)

func recognizerProfileSnapshot(profile RecognizerProfile, adapter ProviderAdapter, prompt PromptVersion, authorContext AuthorRecognitionContext) string {
	var params any
	if err := json.Unmarshal([]byte(profile.ParamsJSON), &params); err != nil {
		params = map[string]any{}
	}
	paramsRaw, _ := json.Marshal(params)
	snapshot := recognizerRunSnapshot{
		ProfileID: profile.ID, ProfileName: profile.Name, Driver: profile.Driver, BaseURL: profile.BaseURL,
		Model: profile.Model, ParamsJSON: profile.ParamsJSON, Params: paramsRaw,
		APIKeyConfigured:  profile.APIKeySet || strings.TrimSpace(profile.APIKey) != "",
		ProviderAdapterID: adapter.ID, ProviderAdapterName: adapter.Name, ProviderEngine: adapter.Engine,
		Endpoint: adapter.Endpoint, AuthType: adapter.AuthType, TimeoutSeconds: adapter.TimeoutSeconds,
		RequestConfigJSON: adapter.RequestConfigJSON, ResponseConfigJSON: adapter.ResponseConfigJSON,
		PromptVersionID: prompt.ID, PromptVersion: prompt.Version, PromptSHA256: prompt.SHA256, PromptText: prompt.Content,
		AuthorProfileID: authorContext.ProfileID, AuthorPromptContext: boundedRunes(authorContext.PromptContext, maxAuthorPromptContextRunes),
	}
	if adapter.ID != "" {
		snapshot.Model = adapter.Model
		snapshot.Driver = recognizer.EngineGenericHTTPJSON
	}
	if authorContext.ProfileID != "" {
		if len(authorContext.SnapshotJSON) <= maxAuthorContextSnapshot && json.Valid([]byte(authorContext.SnapshotJSON)) {
			snapshot.AuthorContext = json.RawMessage(authorContext.SnapshotJSON)
		} else if strings.TrimSpace(authorContext.SnapshotJSON) != "" {
			snapshot.AuthorContextOmitted = true
		}
	}
	return mustJSON(snapshot)
}

func boundedRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func combinedSnapshotPrompt(snapshot recognizerRunSnapshot) string {
	prompt := strings.TrimSpace(snapshot.PromptText)
	if snapshot.AuthorPromptContext != "" {
		prompt += "\n\n" + snapshot.AuthorPromptContext
	}
	return strings.TrimSpace(prompt)
}

func (a *App) recognizerFromRunSnapshot(ctx context.Context, run RecognitionRun) (recognizer.Recognizer, error) {
	var snapshot recognizerRunSnapshot
	if err := json.Unmarshal([]byte(run.ProfileSnapshotJSON), &snapshot); err != nil {
		if run.ProfileID == "" && run.ProviderAdapterID == "" && run.Driver == "" && strings.TrimSpace(run.ProfileSnapshotJSON) == "" {
			return a.Recognizer(), nil // legacy rows predate immutable run snapshots
		}
		return nil, fmt.Errorf("decode recognition run snapshot: %w", err)
	}
	if snapshot.ParamsJSON == "" && len(snapshot.Params) > 0 {
		snapshot.ParamsJSON = string(snapshot.Params)
	}
	if strings.TrimSpace(snapshot.PromptText) == "" && snapshot.PromptVersionID != "" {
		prompt, err := a.Store.GetPromptVersion(ctx, snapshot.PromptVersionID)
		if err != nil {
			return nil, fmt.Errorf("load immutable prompt snapshot %s: %w", snapshot.PromptVersionID, err)
		}
		if snapshot.PromptSHA256 != "" && prompt.SHA256 != snapshot.PromptSHA256 {
			return nil, errors.New("stored prompt snapshot hash no longer matches the original run")
		}
		snapshot.PromptText = prompt.Content
	}
	promptText := combinedSnapshotPrompt(snapshot)
	if snapshot.ProviderAdapterID != "" {
		current, err := a.Store.GetProviderAdapter(ctx, snapshot.ProviderAdapterID)
		if err != nil {
			return nil, fmt.Errorf("provider adapter %s required by the original run is unavailable: %w", snapshot.ProviderAdapterID, err)
		}
		if !current.IsEnabled {
			return nil, fmt.Errorf("provider adapter %s required by the original run is disabled", snapshot.ProviderAdapterID)
		}
		adapter := ProviderAdapter{
			ID: snapshot.ProviderAdapterID, Name: snapshot.ProviderAdapterName, Engine: snapshot.ProviderEngine,
			Endpoint: snapshot.Endpoint, Model: snapshot.Model, AuthType: snapshot.AuthType, Secret: current.Secret,
			TimeoutSeconds: snapshot.TimeoutSeconds, RequestConfigJSON: snapshot.RequestConfigJSON,
			ResponseConfigJSON: snapshot.ResponseConfigJSON, IsEnabled: true,
		}
		return a.registry.BuildProviderAdapter(providerManifest(adapter, promptText, snapshot.PromptVersion))
	}
	driver := snapshot.Driver
	if driver == "" {
		driver = run.Driver
	}
	secret := ""
	if driver == recognizer.DriverOpenAICompatible {
		if snapshot.ProfileID != "" {
			current, err := a.Store.GetRecognizerProfile(ctx, snapshot.ProfileID)
			if err != nil {
				return nil, fmt.Errorf("recognizer profile %s required by the original run is unavailable: %w", snapshot.ProfileID, err)
			}
			secret = current.APIKey
		} else {
			secret = recognizer.RetrySecret(a.Recognizer())
		}
		if strings.TrimSpace(secret) == "" {
			return nil, errors.New("the current credential for the original OpenAI-compatible run is unavailable")
		}
	}
	if driver != recognizer.DriverMock && driver != recognizer.DriverOpenAICompatible {
		if snapshot.ProfileID != "" {
			return nil, fmt.Errorf("recognizer profile snapshot uses unsupported driver %q", driver)
		}
		current := a.Recognizer()
		if run.Provider != "" && current.Provider() != run.Provider {
			return nil, fmt.Errorf("legacy recognizer provider changed from %q to %q", run.Provider, current.Provider())
		}
		if run.Model != "" && current.Model() != run.Model {
			return nil, fmt.Errorf("legacy recognizer model changed from %q to %q", run.Model, current.Model())
		}
		return current, nil
	}
	if driver == recognizer.DriverOpenAICompatible && strings.TrimSpace(promptText) == "" {
		return nil, errors.New("recognition run snapshot does not contain the original prompt text")
	}
	return a.registry.Build(driver, recognizer.ProfileConfig{
		BaseURL: snapshot.BaseURL, APIKey: secret, Model: snapshot.Model, ParamsJSON: snapshot.ParamsJSON,
		PromptText: promptText, PromptVersion: snapshot.PromptVersion,
	})
}

func (a *App) MergeRecognitionCandidates(ctx context.Context, pageID string, resultIDs []string, profileID string) (CandidateMerge, error) {
	page, err := a.Store.GetPage(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	if len(resultIDs) < 2 {
		return CandidateMerge{}, errors.New("at least two recognition_result ids are required")
	}
	if len(resultIDs) > 8 {
		return CandidateMerge{}, errors.New("at most eight recognition results can be merged at once")
	}
	unique := make(map[string]bool, len(resultIDs))
	for _, id := range resultIDs {
		id = strings.TrimSpace(id)
		if id == "" || unique[id] {
			return CandidateMerge{}, errors.New("result ids must be non-empty and distinct")
		}
		unique[id] = true
	}
	allResults, err := a.Store.ListRecognitionResults(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	byID := make(map[string]RecognitionResult, len(allResults))
	for _, result := range allResults {
		byID[result.ID] = result
	}
	candidates := make([]string, 0, len(resultIDs))
	totalBytes := 0
	for _, id := range resultIDs {
		result, ok := byID[id]
		if !ok {
			return CandidateMerge{}, fmt.Errorf("recognition result %q does not belong to page", id)
		}
		candidates = append(candidates, result.Text)
		totalBytes += len(result.Text)
		if totalBytes > 256_000 {
			return CandidateMerge{}, errors.New("candidate merge input exceeds 256 KB")
		}
	}
	profile, hasProfile, err := a.resolveRecognizerProfile(ctx, profileID)
	if err != nil {
		return CandidateMerge{}, err
	}
	var rec recognizer.Recognizer
	if hasProfile {
		prompt, err := a.resolveRunPrompt(ctx, "", profile.PromptVersionID)
		if err != nil {
			return CandidateMerge{}, err
		}
		rec, err = a.registry.Build(profile.Driver, recognizer.ProfileConfig{
			BaseURL: profile.BaseURL, APIKey: profile.APIKey, Model: profile.Model, ParamsJSON: profile.ParamsJSON,
			PromptText: prompt.Content, PromptVersion: prompt.Version,
		})
		if err != nil {
			return CandidateMerge{}, err
		}
	} else {
		rec = a.Recognizer()
		profile = RecognizerProfile{Driver: rec.Provider(), Name: "legacy-settings", Model: rec.Model()}
	}
	merger, ok := rec.(recognizer.CandidateMerger)
	if !ok {
		return CandidateMerge{}, fmt.Errorf("recognizer driver %q does not support candidate merging", profile.Driver)
	}
	merged, err := merger.MergeCandidates(ctx, recognizer.CandidateMergeInput{PageID: pageID, Candidates: candidates})
	if err != nil {
		return CandidateMerge{}, err
	}
	if err := recognizer.ValidateConservativeMerge(merged.Text, candidates); err != nil {
		return CandidateMerge{}, err
	}
	baseID, err := a.effectiveBaseVersionID(ctx, pageID)
	if err != nil {
		return CandidateMerge{}, err
	}
	createdAt := now()
	version := TextVersion{
		ID: newID("txt"), DocumentID: page.DocumentID, PageID: pageID, Kind: "candidate", BaseVersionID: baseID,
		Text: strings.TrimSpace(merged.Text), Status: "draft", CreatedBy: "conservative-merge", CreatedAt: createdAt,
	}
	merge := CandidateMerge{
		ID: newID("merge"), PageID: pageID, TextVersionID: version.ID, SourceResultIDs: append([]string(nil), resultIDs...),
		RecognizerProfileID: profile.ID, Driver: profile.Driver, PromptVersion: recognizer.ConservativeMergePromptVersion,
		PromptHash: recognizer.MergePromptHash(), RawResponse: string(merged.RawResponse), CreatedAt: createdAt, TextVersion: version,
	}
	merge.Segments = conservativeMergeLineage(merge.ID, version.Text, resultIDs, byID)
	if strings.TrimSpace(merge.RawResponse) == "" {
		merge.RawResponse = "{}"
	}
	if err := a.Store.CreateCandidateMerge(ctx, merge, version); err != nil {
		return CandidateMerge{}, err
	}
	_ = a.Store.RecomputeDocumentStatus(ctx, page.DocumentID)
	return merge, nil
}

func conservativeMergeLineage(mergeID, mergedText string, resultIDs []string, results map[string]RecognitionResult) []CandidateMergeSegment {
	type lineSpan struct {
		text      string
		byteStart int
		byteEnd   int
	}
	lineSpans := func(value string) []lineSpan {
		spans := []lineSpan{}
		for offset := 0; ; {
			end := len(value)
			next := len(value)
			if relative := strings.IndexByte(value[offset:], '\n'); relative >= 0 {
				end = offset + relative
				next = end + 1
			}
			raw := value[offset:end]
			normalized := strings.TrimSpace(raw)
			if normalized != "" {
				contentPos := strings.Index(raw, normalized)
				spans = append(spans, lineSpan{
					text: normalized, byteStart: offset + contentPos,
					byteEnd: offset + contentPos + len(normalized),
				})
			}
			if next >= len(value) {
				break
			}
			offset = next
		}
		return spans
	}
	utf16Offset := func(value string, byteOffset int) int {
		return len(utf16.Encode([]rune(value[:byteOffset])))
	}

	sourceLines := map[string][]lineSpan{}
	used := map[string]map[int]bool{}
	for _, resultID := range resultIDs {
		sourceLines[resultID] = lineSpans(results[resultID].Text)
		used[resultID] = map[int]bool{}
	}
	segments := []CandidateMergeSegment{}
	for _, output := range lineSpans(mergedText) {
		found := false
		for _, resultID := range resultIDs {
			result := results[resultID]
			for index, source := range sourceLines[resultID] {
				if used[resultID][index] || source.text != output.text {
					continue
				}
				segments = append(segments, CandidateMergeSegment{
					ID: newID("mseg"), CandidateMergeID: mergeID, Ordinal: len(segments), SourceResultID: resultID,
					SourceStart: utf16Offset(result.Text, source.byteStart), SourceEnd: utf16Offset(result.Text, source.byteEnd),
					OutputStart: utf16Offset(mergedText, output.byteStart), OutputEnd: utf16Offset(mergedText, output.byteEnd), Text: output.text,
				})
				used[resultID][index] = true
				found = true
				break
			}
			if found {
				break
			}
		}
	}
	return segments
}

func (a *App) effectiveBaseVersionID(ctx context.Context, pageID string) (string, error) {
	effective, ok, err := a.Store.EffectiveTextForPage(ctx, pageID)
	if err != nil || !ok {
		return "", err
	}
	if _, err := a.Store.GetTextVersion(ctx, effective.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// EffectiveTextForPage falls back to a synthetic raw_selected value
			// whose ID is a recognition_result ID, not a text_versions FK.
			return "", nil
		}
		return "", err
	}
	return effective.ID, nil
}
