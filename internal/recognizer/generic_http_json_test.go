package recognizer

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tinyProviderPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestGenericHTTPJSONNormalizesConfiguredPathsWithoutLeakingSecret(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(imagePath, tinyProviderPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := NewGenericHTTPJSON(ProviderManifest{
		ID: "adapter_test", Name: "Test OCR", Engine: EngineGenericHTTPJSON,
		Endpoint: "https://ocr.example.com/v1/recognize", Model: "handwriting-v2",
		AuthType: "bearer", Secret: "top-secret", TimeoutSeconds: 30,
		RequestConfigJSON:  `{"model_path":"input.model","prompt_path":"input.prompt","image_path":"input.image","metadata_path":"input.page","image_format":"data_url","static":{"mode":"ocr"}}`,
		ResponseConfigJSON: `{"text_path":"result.transcription","confidence_path":"result.score","metadata_path":"trace"}`,
		PromptText:         "只转录可见文字", PromptVersion: "prompt-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.ConfigJSON(), "top-secret") {
		t.Fatalf("config leaked secret: %s", rec.ConfigJSON())
	}
	rec.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer top-secret" {
			t.Fatalf("authorization = %q", got)
		}
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		input := body["input"].(map[string]any)
		if input["model"] != "handwriting-v2" || input["prompt"] != "只转录可见文字" {
			t.Fatalf("request input = %#v", input)
		}
		if image, _ := input["image"].(string); !strings.HasPrefix(image, "data:image/png;base64,") {
			t.Fatalf("image = %q", image)
		}
		page := input["page"].(map[string]any)
		if page["document_id"] != "doc" || page["page_id"] != "page" || page["page_no"] != float64(3) {
			t.Fatalf("page metadata = %#v", page)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"X-Request-Id": []string{"req-123"}},
			Body:    io.NopCloser(strings.NewReader(`{"result":{"transcription":"识别文本","score":87.5},"trace":{"layout":"single-column"}}`)),
			Request: req,
		}, nil
	})
	result, err := rec.RecognizePage(context.Background(), PageInput{
		DocumentID: "doc", PageID: "page", PageNo: 3, ImagePath: imagePath, Width: 10, Height: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "识别文本" || result.Confidence == nil || *result.Confidence != 0.875 {
		t.Fatalf("result = %+v", result)
	}
	if result.Metadata["confidence_source"] != "result.score" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	providerMetadata := result.Metadata["provider_metadata"].(map[string]any)
	if providerMetadata["layout"] != "single-column" {
		t.Fatalf("provider metadata = %#v", providerMetadata)
	}
}

func TestProviderEndpointSSRFValidation(t *testing.T) {
	valid := []string{"https://ocr.example.com/v1/recognize", "https://[2606:4700:4700::1111]/ocr"}
	for _, endpoint := range valid {
		if _, err := ValidateProviderEndpoint(endpoint); err != nil {
			t.Fatalf("valid endpoint %q: %v", endpoint, err)
		}
	}
	invalid := []string{
		"http://ocr.example.com", "https://user:pass@ocr.example.com", "https://ocr.example.com/path?token=x",
		"https://ocr.example.com/#fragment", "https://localhost/ocr", "https://127.0.0.1/ocr",
		"https://10.0.0.1/ocr", "https://169.254.169.254/latest/meta-data", "file:///tmp/provider",
		"https://192.0.2.10/ocr", "https://198.51.100.20/ocr", "https://203.0.113.30/ocr",
		"https://100.64.0.1/ocr", "https://[2001:db8::1]/ocr",
	}
	for _, endpoint := range invalid {
		if _, err := ValidateProviderEndpoint(endpoint); err == nil {
			t.Fatalf("endpoint %q should be rejected", endpoint)
		}
	}
}

func TestGenericProviderRejectsSecretsInStaticRequestJSON(t *testing.T) {
	_, err := NewGenericHTTPJSON(ProviderManifest{
		ID: "unsafe", Name: "unsafe", Engine: EngineGenericHTTPJSON,
		Endpoint: "https://ocr.example.com/v1", Model: "model", AuthType: "none",
		RequestConfigJSON: `{"static":{"headers":{"api_key":"must-not-enter-snapshot"}}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "credential-like") {
		t.Fatalf("error = %v", err)
	}
}

func TestGenericProviderRejectsUnknownConfigFields(t *testing.T) {
	_, err := NewGenericHTTPJSON(ProviderManifest{
		ID: "unknown", Name: "unknown", Engine: EngineGenericHTTPJSON,
		Endpoint: "https://ocr.example.com/v1", Model: "model", AuthType: "none",
		RequestConfigJSON: `{"api_key":"must-not-be-persisted"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("error = %v", err)
	}
}

func TestGenericProviderRejectsCredentialValuesBehindGenericStaticKeys(t *testing.T) {
	for _, request := range []string{
		`{"static":{"key":"ordinary-looking-name"}}`,
		`{"static":{"auth":"ordinary-looking-secret"}}`,
		`{"static":{"headers":{"X-Custom-Auth":"ordinary-looking-secret"}}}`,
		`{"static":{"header":"sk-secret-value"}}`,
		`{"static":{"subscription_key":"secret"}}`,
	} {
		_, err := NewGenericHTTPJSON(ProviderManifest{
			ID: "unsafe-value", Name: "unsafe-value", Engine: EngineGenericHTTPJSON,
			Endpoint: "https://ocr.example.com/v1", Model: "model", AuthType: "none", RequestConfigJSON: request,
		})
		if err == nil || !strings.Contains(err.Error(), "credential-like") {
			t.Fatalf("request %s error = %v", request, err)
		}
	}
}
