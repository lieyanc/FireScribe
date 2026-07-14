package recognizer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lieyan/firescribe/internal/pageproc"
)

const maxGenericProviderResponse = 32 << 20

// ProviderManifest is the complete allow-listed input accepted by the built-in
// generic HTTP engine. It is data, not code: JSON paths only select fields in a
// request/response object and cannot invoke local files, commands, templates,
// or dynamically loaded modules.
type ProviderManifest struct {
	ID                 string
	Name               string
	Engine             string
	Endpoint           string
	Model              string
	AuthType           string
	Secret             string
	TimeoutSeconds     int
	RequestConfigJSON  string
	ResponseConfigJSON string
	PromptText         string
	PromptVersion      string
}

type genericRequestConfig struct {
	ModelPath    string         `json:"model_path"`
	PromptPath   string         `json:"prompt_path"`
	ImagePath    string         `json:"image_path"`
	MetadataPath string         `json:"metadata_path"`
	ImageFormat  string         `json:"image_format"`
	MaxImageEdge int            `json:"max_image_edge"`
	Static       map[string]any `json:"static"`
}

type genericResponseConfig struct {
	TextPath       string `json:"text_path"`
	ConfidencePath string `json:"confidence_path"`
	MetadataPath   string `json:"metadata_path"`
}

type GenericHTTPJSONRecognizer struct {
	manifest ProviderManifest
	request  genericRequestConfig
	response genericResponseConfig
	client   *http.Client
}

func NewGenericHTTPJSON(manifest ProviderManifest) (*GenericHTTPJSONRecognizer, error) {
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Engine = strings.TrimSpace(manifest.Engine)
	manifest.Endpoint = strings.TrimSpace(manifest.Endpoint)
	manifest.Model = strings.TrimSpace(manifest.Model)
	manifest.AuthType = strings.TrimSpace(manifest.AuthType)
	if manifest.Engine == "" {
		manifest.Engine = EngineGenericHTTPJSON
	}
	if manifest.Engine != EngineGenericHTTPJSON {
		return nil, fmt.Errorf("unsupported provider adapter engine %q", manifest.Engine)
	}
	if manifest.ID == "" || manifest.Name == "" {
		return nil, errors.New("provider adapter id and name are required")
	}
	if _, err := ValidateProviderEndpoint(manifest.Endpoint); err != nil {
		return nil, err
	}
	if manifest.Model == "" {
		return nil, errors.New("provider adapter model is required")
	}
	if manifest.TimeoutSeconds == 0 {
		manifest.TimeoutSeconds = 120
	}
	if manifest.TimeoutSeconds < 5 || manifest.TimeoutSeconds > 3600 {
		return nil, errors.New("provider adapter timeout_seconds must be between 5 and 3600")
	}
	switch manifest.AuthType {
	case "", "none":
		manifest.AuthType = "none"
	case "bearer", "x-api-key":
		if strings.TrimSpace(manifest.Secret) == "" {
			return nil, fmt.Errorf("provider adapter secret is required for auth_type %q", manifest.AuthType)
		}
	default:
		return nil, fmt.Errorf("unsupported provider adapter auth_type %q", manifest.AuthType)
	}

	reqCfg := genericRequestConfig{
		ModelPath: "model", PromptPath: "prompt", ImagePath: "image", MetadataPath: "page",
		ImageFormat: "data_url", Static: map[string]any{},
	}
	if raw := strings.TrimSpace(manifest.RequestConfigJSON); raw != "" {
		if err := validateJSONObjectKeys([]byte(raw), "request_config_json", map[string]bool{
			"model_path": true, "prompt_path": true, "image_path": true, "metadata_path": true,
			"image_format": true, "max_image_edge": true, "static": true,
		}); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &reqCfg); err != nil {
			return nil, fmt.Errorf("parse provider request_config_json: %w", err)
		}
	}
	if reqCfg.Static == nil {
		reqCfg.Static = map[string]any{}
	}
	if key := sensitiveStaticKey(reqCfg.Static); key != "" {
		return nil, fmt.Errorf("provider adapter static request JSON must not contain credential-like key %q; use auth_type and the write-only secret field", key)
	}
	if reqCfg.ImageFormat == "" {
		reqCfg.ImageFormat = "data_url"
	}
	if reqCfg.ImageFormat != "data_url" && reqCfg.ImageFormat != "base64" {
		return nil, errors.New("provider adapter image_format must be data_url or base64")
	}
	if reqCfg.MaxImageEdge < 0 || reqCfg.MaxImageEdge > 8192 {
		return nil, errors.New("provider adapter max_image_edge must be between 0 and 8192")
	}
	for label, path := range map[string]string{
		"model_path": reqCfg.ModelPath, "prompt_path": reqCfg.PromptPath,
		"image_path": reqCfg.ImagePath, "metadata_path": reqCfg.MetadataPath,
	} {
		if err := validateJSONPath(path); err != nil {
			return nil, fmt.Errorf("provider adapter %s: %w", label, err)
		}
	}

	respCfg := genericResponseConfig{TextPath: "text", ConfidencePath: "confidence", MetadataPath: "metadata"}
	if raw := strings.TrimSpace(manifest.ResponseConfigJSON); raw != "" {
		if err := validateJSONObjectKeys([]byte(raw), "response_config_json", map[string]bool{
			"text_path": true, "confidence_path": true, "metadata_path": true,
		}); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &respCfg); err != nil {
			return nil, fmt.Errorf("parse provider response_config_json: %w", err)
		}
	}
	if strings.TrimSpace(respCfg.TextPath) == "" {
		return nil, errors.New("provider adapter response text_path is required")
	}
	for label, path := range map[string]string{
		"text_path": respCfg.TextPath, "confidence_path": respCfg.ConfidencePath, "metadata_path": respCfg.MetadataPath,
	} {
		if path != "" {
			if err := validateJSONPath(path); err != nil {
				return nil, fmt.Errorf("provider adapter %s: %w", label, err)
			}
		}
	}

	return &GenericHTTPJSONRecognizer{
		manifest: manifest,
		request:  reqCfg,
		response: respCfg,
		client: &http.Client{
			Timeout: time.Duration(manifest.TimeoutSeconds) * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           safeProviderDialContext,
				ForceAttemptHTTP2:     true,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: time.Duration(manifest.TimeoutSeconds) * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func validateJSONObjectKeys(raw []byte, label string, allowed map[string]bool) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("parse provider %s: %w", label, err)
	}
	if object == nil {
		return fmt.Errorf("provider %s must be a JSON object", label)
	}
	for key := range object {
		if !allowed[key] {
			return fmt.Errorf("provider %s contains unsupported field %q", label, key)
		}
	}
	return nil
}

func sensitiveStaticKey(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
			if normalized == "key" || normalized == "auth" || strings.HasSuffix(normalized, "auth") ||
				strings.HasSuffix(normalized, "authentication") || strings.HasSuffix(normalized, "apikey") ||
				strings.HasSuffix(normalized, "accesskey") || strings.HasSuffix(normalized, "subscriptionkey") {
				return key
			}
			for _, marker := range []string{"apikey", "token", "secret", "authorization", "password", "credential"} {
				if strings.Contains(normalized, marker) {
					return key
				}
			}
			if nested := sensitiveStaticKey(child); nested != "" {
				return nested
			}
		}
	case []any:
		for _, child := range current {
			if nested := sensitiveStaticKey(child); nested != "" {
				return nested
			}
		}
	case string:
		normalized := strings.TrimSpace(current)
		lower := strings.ToLower(normalized)
		for _, prefix := range []string{"sk-", "pk-", "ghp_", "github_pat_", "aiza", "bearer ", "basic "} {
			if strings.HasPrefix(lower, prefix) {
				return "credential-like value"
			}
		}
	}
	return ""
}

func (r *GenericHTTPJSONRecognizer) Name() string { return r.manifest.Name }

func (r *GenericHTTPJSONRecognizer) Provider() string {
	return EngineGenericHTTPJSON + ":" + r.manifest.ID
}

func (r *GenericHTTPJSONRecognizer) Model() string { return r.manifest.Model }

func (r *GenericHTTPJSONRecognizer) PromptVersion() string {
	prompt := strings.TrimSpace(r.manifest.PromptText)
	sum := sha256.Sum256([]byte(prompt))
	version := strings.TrimSpace(r.manifest.PromptVersion)
	if version == "" {
		version = "prompt"
	}
	return version + "#" + hex.EncodeToString(sum[:4])
}

func (r *GenericHTTPJSONRecognizer) PromptSnapshotText() string { return r.manifest.PromptText }

func (r *GenericHTTPJSONRecognizer) WithPromptSnapshot(text, version string) Recognizer {
	manifest := r.manifest
	manifest.PromptText = text
	manifest.PromptVersion = version
	clone, err := NewGenericHTTPJSON(manifest)
	if err != nil {
		return r
	}
	return clone
}

func (r *GenericHTTPJSONRecognizer) ConfigJSON() string {
	var requestConfig, responseConfig any
	_ = json.Unmarshal([]byte(r.manifest.RequestConfigJSON), &requestConfig)
	_ = json.Unmarshal([]byte(r.manifest.ResponseConfigJSON), &responseConfig)
	_, hash := hashPrompt(r.manifest.PromptText)
	raw, _ := json.Marshal(map[string]any{
		"adapter_id": r.manifest.ID, "engine": r.manifest.Engine, "endpoint": r.manifest.Endpoint,
		"model": r.manifest.Model, "auth_type": r.manifest.AuthType, "timeout_seconds": r.manifest.TimeoutSeconds,
		"request_config": requestConfig, "response_config": responseConfig,
		"prompt_version": r.manifest.PromptVersion, "prompt_hash": hash,
	})
	return string(raw)
}

func (r *GenericHTTPJSONRecognizer) RecognizePage(ctx context.Context, input PageInput) (RecognitionResult, error) {
	mimeType, imageData, err := pageproc.PrepareForUpload(input.ImagePath, r.request.MaxImageEdge)
	if err != nil {
		return RecognitionResult{}, fmt.Errorf("prepare page image: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(imageData)
	imageValue := encoded
	if r.request.ImageFormat == "data_url" {
		imageValue = "data:" + mimeType + ";base64," + encoded
	}
	body := cloneJSONObject(r.request.Static)
	for path, value := range map[string]any{
		r.request.ModelPath:  r.manifest.Model,
		r.request.PromptPath: r.manifest.PromptText,
		r.request.ImagePath:  imageValue,
		r.request.MetadataPath: map[string]any{
			"document_id": input.DocumentID, "page_id": input.PageID, "page_no": input.PageNo,
			"width": input.Width, "height": input.Height, "image_mime": mimeType,
		},
	} {
		if err := setJSONPath(body, path, value); err != nil {
			return RecognitionResult{}, err
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return RecognitionResult{}, fmt.Errorf("encode provider request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.manifest.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return RecognitionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch r.manifest.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+r.manifest.Secret)
	case "x-api-key":
		req.Header.Set("X-API-Key", r.manifest.Secret)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return RecognitionResult{}, ctx.Err()
		}
		return RecognitionResult{}, fmt.Errorf("request provider adapter: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGenericProviderResponse+1))
	if err != nil {
		return RecognitionResult{}, fmt.Errorf("read provider adapter response: %w", err)
	}
	if len(raw) > maxGenericProviderResponse {
		return RecognitionResult{}, errors.New("provider adapter response exceeds 32 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RecognitionResult{}, fmt.Errorf("provider adapter returned %s: %s", resp.Status, bodySnippet(raw))
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return RecognitionResult{}, fmt.Errorf("parse provider adapter response: %w: %s", err, bodySnippet(raw))
	}
	textValue, ok := getJSONPath(parsed, r.response.TextPath)
	if !ok {
		return RecognitionResult{}, fmt.Errorf("provider adapter response missing text at %q", r.response.TextPath)
	}
	text, ok := textValue.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return RecognitionResult{}, fmt.Errorf("provider adapter response text at %q is empty or not a string", r.response.TextPath)
	}
	metadata := map[string]any{
		"adapter_id": r.manifest.ID, "engine": r.manifest.Engine, "upload_mime": mimeType,
		"response_text_path": r.response.TextPath,
	}
	if headers := auditResponseHeaders(resp.Header); len(headers) > 0 {
		metadata["response_headers"] = headers
	}
	var confidence *float64
	if r.response.ConfidencePath != "" {
		if value, found := getJSONPath(parsed, r.response.ConfidencePath); found {
			if normalized, ok := normalizedConfidence(value); ok {
				confidence = &normalized
				metadata["confidence_source"] = r.response.ConfidencePath
			}
		}
	}
	if r.response.MetadataPath != "" {
		if value, found := getJSONPath(parsed, r.response.MetadataPath); found {
			metadata["provider_metadata"] = value
		}
	}
	return RecognitionResult{Text: strings.TrimSpace(text), Confidence: confidence, RawJSON: raw, Metadata: metadata}, nil
}

// ValidateProviderEndpoint performs the persistent-manifest side of the SSRF
// guard. The runtime transport separately rejects private/reserved DNS answers.
func ValidateProviderEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("provider adapter endpoint must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("provider adapter endpoint must not contain userinfo, query parameters, or fragments")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || host == "metadata.google.internal" {
		return nil, errors.New("provider adapter endpoint host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicProviderIP(ip) {
		return nil, errors.New("provider adapter endpoint must not use a private or reserved IP address")
	}
	return parsed, nil
}

func safeProviderDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("provider adapter host %q resolved to no addresses", host)
	}
	for _, address := range addresses {
		if !isPublicProviderIP(address.IP) {
			return nil, fmt.Errorf("provider adapter host %q resolved to a private or reserved address", host)
		}
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func isPublicProviderIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range reservedProviderNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

var reservedProviderNetworks = mustProviderNetworks(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	"100::/64", "2001:10::/28", "2001:db8::/32", "64:ff9b:1::/48",
)

func mustProviderNetworks(values ...string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		result = append(result, network)
	}
	return result
}

func validateJSONPath(path string) error {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) == 0 || len(parts) > 16 {
		return errors.New("JSON path must contain 1 to 16 segments")
	}
	for _, part := range parts {
		if part == "" || len(part) > 128 {
			return errors.New("JSON path segments must be non-empty and at most 128 bytes")
		}
		for _, char := range part {
			if !(char == '_' || char == '-' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
				return errors.New("JSON paths only support letters, digits, underscore, dash, and dot separators")
			}
		}
	}
	return nil
}

func setJSONPath(root map[string]any, path string, value any) error {
	if err := validateJSONPath(path); err != nil {
		return err
	}
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, exists := current[part]
		if !exists {
			child := map[string]any{}
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("provider adapter JSON path %q conflicts with a non-object value", path)
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func getJSONPath(root any, path string) (any, bool) {
	current := root
	for _, part := range strings.Split(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			current, _ = value[part]
			if current == nil {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func normalizedConfidence(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if number > 1 && number <= 100 {
		number /= 100
	}
	if number < 0 || number > 1 {
		return 0, false
	}
	return number, true
}

func cloneJSONObject(input map[string]any) map[string]any {
	raw, _ := json.Marshal(input)
	output := map[string]any{}
	_ = json.Unmarshal(raw, &output)
	return output
}
