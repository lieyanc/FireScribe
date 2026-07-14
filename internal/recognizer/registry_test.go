package recognizer

import "testing"

func TestRegistryRejectsCredentialsInBaseURL(t *testing.T) {
	registry := NewRegistry()
	for _, baseURL := range []string{
		"https://user:secret@example.com/v1",
		"https://example.com/v1?token=secret",
		"https://example.com/v1#secret",
	} {
		if _, err := registry.Build(DriverOpenAICompatible, ProfileConfig{
			BaseURL: baseURL, APIKey: "key", Model: "model", ParamsJSON: `{}`,
		}); err == nil {
			t.Fatalf("base URL %q was accepted", baseURL)
		}
	}
}
