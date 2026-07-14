package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lieyan/firescribe/internal/app"
	"github.com/lieyan/firescribe/internal/config"
	"github.com/lieyan/firescribe/internal/recognizer"
)

type createPromptVersionRequest struct {
	Version string `json:"version"`
	Content string `json:"content"`
}

func normalizedPrompt(version, content string) (string, string, string, error) {
	version = strings.TrimSpace(version)
	content = strings.TrimSpace(content)
	if version == "" {
		return "", "", "", errors.New("prompt version is required")
	}
	if len(version) > 128 {
		return "", "", "", errors.New("prompt version must not exceed 128 characters")
	}
	if content == "" {
		return "", "", "", errors.New("prompt content is required")
	}
	if len(content) > 1024*1024 {
		return "", "", "", errors.New("prompt content must not exceed 1 MiB")
	}
	sum := sha256.Sum256([]byte(content))
	return version, content, hex.EncodeToString(sum[:]), nil
}

func currentPrompt(cfg config.Config) (string, string, string, error) {
	content := ""
	if path := strings.TrimSpace(cfg.PromptPath); path != "" {
		raw, err := os.ReadFile(path)
		if err == nil {
			content = string(raw)
		} else if !os.IsNotExist(err) {
			return "", "", "", err
		}
	}
	if strings.TrimSpace(content) == "" {
		content = recognizer.DefaultPrompt
	}
	version := strings.TrimSpace(cfg.OpenAI.PromptVersion)
	if version == "" {
		version = "prompt"
	}
	return normalizedPrompt(version, content)
}

// syncCurrentPrompt keeps direct prompt_path edits and the legacy settings
// PUT API compatible with the version library. It intentionally reads the
// prompt file on every call, just like the recognizer does.
func (s *Server) syncCurrentPrompt(ctx context.Context, cfg config.Config) (app.PromptVersion, error) {
	if s.app == nil || s.app.Store == nil {
		return app.PromptVersion{}, errors.New("application store is not configured")
	}
	version, content, digest, err := currentPrompt(cfg)
	if err != nil {
		return app.PromptVersion{}, err
	}
	return s.app.Store.EnsureActivePromptVersion(ctx, version, content, digest)
}

func (s *Server) listPromptVersions(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "settings runtime is not configured"})
		return
	}
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	if _, err := s.syncCurrentPrompt(r.Context(), s.runtime.Config()); err != nil {
		writeError(w, fmt.Errorf("sync prompt library: %w", err))
		return
	}
	versions, err := s.app.Store.ListPromptVersions(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) createPromptVersion(w http.ResponseWriter, r *http.Request) {
	var req createPromptVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("parse prompt version: %w", err))
		return
	}
	version, content, digest, err := normalizedPrompt(req.Version, req.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, err := s.app.Store.CreatePromptVersion(r.Context(), version, content, digest)
	if errors.Is(err, app.ErrPromptVersionExists) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) activatePromptVersion(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "settings runtime is not configured"})
		return
	}
	s.promptMu.Lock()
	defer s.promptMu.Unlock()

	item, err := s.app.Store.GetPromptVersion(r.Context(), chi.URLParam(r, "promptID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "prompt version not found"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}

	previous := s.runtime.Config()
	path := strings.TrimSpace(previous.PromptPath)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt_path is not configured"})
		return
	}
	oldRaw, oldReadErr := os.ReadFile(path)
	oldExisted := oldReadErr == nil
	if oldReadErr != nil && !os.IsNotExist(oldReadErr) {
		writeError(w, fmt.Errorf("read current prompt: %w", oldReadErr))
		return
	}
	if err := writePromptFile(path, []byte(item.Content)); err != nil {
		writeError(w, fmt.Errorf("activate prompt: %w", err))
		return
	}

	if _, err = s.runtime.Apply(func(cfg *config.Config) error {
		cfg.OpenAI.PromptVersion = item.Version
		return nil
	}); err != nil {
		_ = restorePromptFile(path, oldRaw, oldExisted)
		writeError(w, fmt.Errorf("activate prompt: %w", err))
		return
	}
	activated, err := s.app.Store.ActivatePromptVersion(r.Context(), item.ID)
	if err != nil {
		_, _ = s.runtime.Apply(func(cfg *config.Config) error {
			cfg.OpenAI.PromptVersion = previous.OpenAI.PromptVersion
			return nil
		})
		_ = restorePromptFile(path, oldRaw, oldExisted)
		writeError(w, fmt.Errorf("activate prompt library record: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, activated)
}

func writePromptFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".firescribe-prompt-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func restorePromptFile(path string, content []byte, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writePromptFile(path, content)
}
