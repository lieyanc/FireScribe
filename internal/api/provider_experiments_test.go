package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
	"github.com/lieyan/firescribe/internal/updater"
)

func TestProviderAdapterMutationsRequireAdminTokenAndNeverEchoSecret(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	router := New(app.New(app.NewStore(conn), files, recognizer.MockRecognizer{}), "", nil,
		UpdateRuntime{Config: updater.Config{AdminToken: "admin-secret"}}).Routes()
	body := []byte(`{"name":"External OCR","engine":"generic-http-json","endpoint":"https://ocr.example.com/v1/recognize","model":"ocr-v2","auth_type":"bearer","secret":"provider-secret","timeout_seconds":30,"request_config_json":"{}","response_config_json":"{\"text_path\":\"result.text\"}","is_enabled":true}`)

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/provider-adapters", bytes.NewReader(body))
	unauthorized.RemoteAddr = "192.0.2.20:12345"
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodPost, "/api/provider-adapters", bytes.NewReader(body))
	authorized.RemoteAddr = "192.0.2.20:12345"
	authorized.Header.Set("Content-Type", "application/json")
	authorized.Header.Set("X-Admin-Token", "admin-secret")
	authorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusCreated {
		t.Fatalf("authorized status = %d body=%s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}
	if strings.Contains(authorizedRecorder.Body.String(), "provider-secret") || strings.Contains(authorizedRecorder.Body.String(), `"secret"`) {
		t.Fatalf("create response leaked secret: %s", authorizedRecorder.Body.String())
	}
	if !strings.Contains(authorizedRecorder.Body.String(), `"secret_set":true`) {
		t.Fatalf("safe secret flag missing: %s", authorizedRecorder.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/provider-adapters", nil)
	list.RemoteAddr = "192.0.2.20:12345"
	list.AddCookie(testSessionCookie(t, router))
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK || strings.Contains(listRecorder.Body.String(), "provider-secret") || strings.Contains(listRecorder.Body.String(), `"secret"`) {
		t.Fatalf("list response unsafe: %d %s", listRecorder.Code, listRecorder.Body.String())
	}
}
