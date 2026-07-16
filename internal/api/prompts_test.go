package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/config"
	"github.com/lieyan/firescribe/internal/db"
	"github.com/lieyan/firescribe/internal/recognizer"
	"github.com/lieyan/firescribe/internal/storage"
	"github.com/lieyan/firescribe/internal/updater"
)

func TestPromptLibraryImportsCreatesActivatesAndTracksExternalEdits(t *testing.T) {
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
	promptPath := filepath.Join(dir, "prompts", "active.txt")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("第一版提示词"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.PromptPath = promptPath
	cfg.UseMockOCR = true
	cfg.OpenAI.PromptVersion = "legacy-v1"
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runtime := config.NewRuntime(cfg)
	application := app.New(app.NewStore(conn), files, recognizer.MockRecognizer{})
	router := New(application, "", runtime, UpdateRuntime{Config: updater.Config{AdminToken: "secret"}}).Routes()
	session := testSessionCookie(t, router)

	versions := requestPromptVersions(t, router, session)
	if len(versions) != 1 || !versions[0].IsActive || versions[0].Version != "legacy-v1" || versions[0].Content != "第一版提示词" {
		t.Fatalf("initial versions = %#v", versions)
	}
	if len(versions[0].SHA256) != 64 {
		t.Fatalf("initial SHA-256 = %q", versions[0].SHA256)
	}

	created := requestPromptMutation(t, router, http.MethodPost, "/api/prompts", map[string]string{
		"version": "library-v2",
		"content": "第二版提示词",
	}, http.StatusCreated)
	if created.IsActive {
		t.Fatal("new prompt version unexpectedly active")
	}
	activated := requestPromptMutation(t, router, http.MethodPost, "/api/prompts/"+created.ID+"/activate", nil, http.StatusOK)
	if !activated.IsActive || activated.Version != "library-v2" {
		t.Fatalf("activated = %#v", activated)
	}
	if got := runtime.Config().OpenAI.PromptVersion; got != "library-v2" {
		t.Fatalf("runtime prompt version = %q", got)
	}
	if raw, err := os.ReadFile(promptPath); err != nil || string(raw) != "第二版提示词" {
		t.Fatalf("active prompt file = %q, %v", raw, err)
	}
	rec := recognizer.NewOpenAI(recognizer.OpenAIConfig{PromptPath: promptPath, PromptVersion: runtime.Config().OpenAI.PromptVersion})
	wantShortHash := activated.SHA256[:8]
	if got := rec.PromptVersion(); got != "library-v2#"+wantShortHash {
		t.Fatalf("recognition prompt version = %q, want library-v2#%s", got, wantShortHash)
	}

	// Direct edits remain hot: the next API read snapshots the changed file
	// under the configured label and marks that exact hash active.
	if err := os.WriteFile(promptPath, []byte("外部直接编辑"), 0o644); err != nil {
		t.Fatal(err)
	}
	versions = requestPromptVersions(t, router, session)
	if len(versions) != 3 || versions[0].Version != "library-v2" || versions[0].Content != "外部直接编辑" || !versions[0].IsActive {
		t.Fatalf("versions after direct edit = %#v", versions)
	}

	// Settings PUT can still update the active prompt file and snapshot it
	// into the version library (model/API keys live under LLM providers now).
	settingsBody := []byte(`{"prompt":"第三版提示词"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(settingsBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("settings PUT status = %d, body = %s", res.Code, res.Body.String())
	}
	versions = requestPromptVersions(t, router, session)
	if versions[0].Content != "第三版提示词" || !versions[0].IsActive {
		t.Fatalf("versions after settings PUT = %#v", versions)
	}
}

func TestPromptLibraryRejectsDuplicateSnapshot(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "firescribe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(conn)
	content := "相同内容"
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	if _, err := store.CreatePromptVersion(context.Background(), "v1", content, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePromptVersion(context.Background(), "v1", content, digest); err != app.ErrPromptVersionExists {
		t.Fatalf("duplicate error = %v", err)
	}
}

func requestPromptVersions(t *testing.T, handler http.Handler, session *http.Cookie) []app.PromptVersion {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/prompts", nil)
	req.AddCookie(session)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /api/prompts status = %d, body = %s", res.Code, res.Body.String())
	}
	var versions []app.PromptVersion
	if err := json.NewDecoder(res.Body).Decode(&versions); err != nil {
		t.Fatal(err)
	}
	return versions
}

func requestPromptMutation(t *testing.T, handler http.Handler, method, path string, body any, wantStatus int) app.PromptVersion {
	t.Helper()
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", "secret")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != wantStatus {
		t.Fatalf("%s %s status = %d, body = %s", method, path, res.Code, res.Body.String())
	}
	var item app.PromptVersion
	if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	return item
}
