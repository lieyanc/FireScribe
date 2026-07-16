package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type storedSecrets struct {
	LLMProviders       map[string]string `json:"llm_providers"`
	RecognizerProfiles map[string]string `json:"recognizer_profiles"`
	ProviderAdapters   map[string]string `json:"provider_adapters"`
}

// ConfigureSecretFile moves legacy credentials out of SQLite and keeps future
// Provider/Adapter secrets in a mode-0600 local file. Database rows retain only
// non-secret configuration and secret-set state is derived from this vault.
func (s *Store) ConfigureSecretFile(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("secret file path is required")
	}
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == path && s.secrets.LLMProviders != nil && s.secrets.RecognizerProfiles != nil && s.secrets.ProviderAdapters != nil {
		return nil
	}
	secrets := storedSecrets{
		LLMProviders:       map[string]string{},
		RecognizerProfiles: map[string]string{},
		ProviderAdapters:   map[string]string{},
	}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &secrets); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if secrets.LLMProviders == nil {
		secrets.LLMProviders = map[string]string{}
	}
	if secrets.RecognizerProfiles == nil {
		secrets.RecognizerProfiles = map[string]string{}
	}
	if secrets.ProviderAdapters == nil {
		secrets.ProviderAdapters = map[string]string{}
	}

	profileRows, err := s.db.QueryContext(ctx, `SELECT id, api_key FROM recognizer_profiles WHERE trim(api_key) <> ''`)
	if err != nil {
		return err
	}
	for profileRows.Next() {
		var id, secret string
		if err := profileRows.Scan(&id, &secret); err != nil {
			_ = profileRows.Close()
			return err
		}
		secrets.RecognizerProfiles[id] = secret
	}
	if err := profileRows.Close(); err != nil {
		return err
	}
	adapterRows, err := s.db.QueryContext(ctx, `SELECT id, secret FROM provider_adapters WHERE trim(secret) <> ''`)
	if err != nil {
		return err
	}
	for adapterRows.Next() {
		var id, secret string
		if err := adapterRows.Scan(&id, &secret); err != nil {
			_ = adapterRows.Close()
			return err
		}
		secrets.ProviderAdapters[id] = secret
	}
	if err := adapterRows.Close(); err != nil {
		return err
	}
	if err := writeStoredSecrets(path, secrets); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE recognizer_profiles SET api_key = '' WHERE api_key <> ''`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE provider_adapters SET secret = '' WHERE secret <> ''`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.secretPath = path
	s.secrets = secrets
	return nil
}

func writeStoredSecrets(path string, secrets storedSecrets) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".secrets-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func (s *Store) providerSecret(id string) string {
	s.secretMu.RLock()
	defer s.secretMu.RUnlock()
	if s.secretPath == "" {
		return ""
	}
	return s.secrets.LLMProviders[id]
}

func (s *Store) profileSecret(id, databaseValue string) string {
	s.secretMu.RLock()
	defer s.secretMu.RUnlock()
	if s.secretPath == "" {
		return databaseValue
	}
	return s.secrets.RecognizerProfiles[id]
}

func (s *Store) adapterSecret(id, databaseValue string) string {
	s.secretMu.RLock()
	defer s.secretMu.RUnlock()
	if s.secretPath == "" {
		return databaseValue
	}
	return s.secrets.ProviderAdapters[id]
}

func (s *Store) saveProviderSecret(id, secret string) error {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == "" {
		// Without a vault file (tests), keep secret only in memory map.
		if s.secrets.LLMProviders == nil {
			s.secrets.LLMProviders = map[string]string{}
		}
		if strings.TrimSpace(secret) == "" {
			delete(s.secrets.LLMProviders, id)
		} else {
			s.secrets.LLMProviders[id] = secret
		}
		return nil
	}
	if strings.TrimSpace(secret) == "" {
		// Empty means "leave unchanged" at the app layer; callers that want to
		// clear must pass a dedicated signal. Here empty is a no-op on update.
		return nil
	}
	if s.secrets.LLMProviders == nil {
		s.secrets.LLMProviders = map[string]string{}
	}
	s.secrets.LLMProviders[id] = secret
	return writeStoredSecrets(s.secretPath, s.secrets)
}

func (s *Store) saveProfileSecret(id, secret string) (string, error) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == "" {
		return secret, nil
	}
	if strings.TrimSpace(secret) == "" {
		delete(s.secrets.RecognizerProfiles, id)
	} else {
		s.secrets.RecognizerProfiles[id] = secret
	}
	return "", writeStoredSecrets(s.secretPath, s.secrets)
}

func (s *Store) saveAdapterSecret(id, secret string) (string, error) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == "" {
		return secret, nil
	}
	if strings.TrimSpace(secret) == "" {
		delete(s.secrets.ProviderAdapters, id)
	} else {
		s.secrets.ProviderAdapters[id] = secret
	}
	return "", writeStoredSecrets(s.secretPath, s.secrets)
}

func (s *Store) deleteProviderSecret(id string) error {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secrets.LLMProviders == nil {
		s.secrets.LLMProviders = map[string]string{}
	}
	delete(s.secrets.LLMProviders, id)
	if s.secretPath == "" {
		return nil
	}
	return writeStoredSecrets(s.secretPath, s.secrets)
}

func (s *Store) deleteProfileSecret(id string) error {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == "" {
		return nil
	}
	delete(s.secrets.RecognizerProfiles, id)
	return writeStoredSecrets(s.secretPath, s.secrets)
}

func (s *Store) deleteAdapterSecret(id string) error {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	if s.secretPath == "" {
		return nil
	}
	delete(s.secrets.ProviderAdapters, id)
	return writeStoredSecrets(s.secretPath, s.secrets)
}
