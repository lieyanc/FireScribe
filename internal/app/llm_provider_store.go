package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) ListLLMProviders(ctx context.Context) ([]LLMProvider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.driver, p.base_url, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM recognizer_profiles m WHERE m.provider_id = p.id) AS model_count
		FROM llm_providers p
		ORDER BY p.updated_at DESC, p.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LLMProvider{}
	for rows.Next() {
		item, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		item.APIKey = s.providerSecret(item.ID)
		item.APIKeySet = strings.TrimSpace(item.APIKey) != ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	item, err := scanLLMProvider(s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.driver, p.base_url, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM recognizer_profiles m WHERE m.provider_id = p.id) AS model_count
		FROM llm_providers p
		WHERE p.id = ?
	`, id))
	if err != nil {
		return LLMProvider{}, err
	}
	item.APIKey = s.providerSecret(item.ID)
	item.APIKeySet = strings.TrimSpace(item.APIKey) != ""
	return item, nil
}

func (s *Store) SaveLLMProvider(ctx context.Context, provider LLMProvider) (LLMProvider, error) {
	if provider.Driver == "mock" || strings.TrimSpace(provider.APIKey) == "" && provider.Driver != "openai-compatible" {
		if err := s.deleteProviderSecret(provider.ID); err != nil {
			return LLMProvider{}, err
		}
	} else if strings.TrimSpace(provider.APIKey) != "" {
		if err := s.saveProviderSecret(provider.ID, provider.APIKey); err != nil {
			return LLMProvider{}, err
		}
	}
	if provider.CreatedAt == "" {
		provider.CreatedAt = now()
	}
	provider.UpdatedAt = now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO llm_providers(id, name, driver, base_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  driver = excluded.driver,
		  base_url = excluded.base_url,
		  updated_at = excluded.updated_at
	`, provider.ID, provider.Name, provider.Driver, provider.BaseURL, provider.CreatedAt, provider.UpdatedAt)
	if err != nil {
		return LLMProvider{}, err
	}
	return s.GetLLMProvider(ctx, provider.ID)
}

func (s *Store) DeleteLLMProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM llm_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return s.deleteProviderSecret(id)
}

func (s *Store) ListRecognizerProfilesByProvider(ctx context.Context, providerID string) ([]RecognizerProfile, error) {
	rows, err := s.db.QueryContext(ctx, recognizerProfileSelect+`
		WHERE p.provider_id = ?
		ORDER BY p.is_default DESC, p.updated_at DESC, p.name
	`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecognizerProfiles(rows, s)
}

func scanLLMProvider(scanner interface{ Scan(...any) error }) (LLMProvider, error) {
	var item LLMProvider
	err := scanner.Scan(&item.ID, &item.Name, &item.Driver, &item.BaseURL, &item.CreatedAt, &item.UpdatedAt, &item.ModelCount)
	return item, err
}

// MigrateProfilesToLLMProviders groups legacy flat profiles into providers and
// moves credentials from profile secrets onto the parent provider.
func (s *Store) MigrateProfilesToLLMProviders(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, driver, base_url, api_key, model, params_json,
		       COALESCE(prompt_version_id, ''), is_default, created_at, updated_at,
		       COALESCE(provider_id, '')
		FROM recognizer_profiles
		ORDER BY created_at, id
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type legacyProfile struct {
		ID, Name, Driver, BaseURL, APIKey, Model, ParamsJSON, PromptVersionID, ProviderID string
		IsDefault                                                                         int
		CreatedAt, UpdatedAt                                                              string
	}
	var legacy []legacyProfile
	for rows.Next() {
		var item legacyProfile
		if err := rows.Scan(&item.ID, &item.Name, &item.Driver, &item.BaseURL, &item.APIKey, &item.Model,
			&item.ParamsJSON, &item.PromptVersionID, &item.IsDefault, &item.CreatedAt, &item.UpdatedAt, &item.ProviderID); err != nil {
			return 0, err
		}
		// Prefer vault secret over any residual DB value.
		if secret := s.profileSecret(item.ID, item.APIKey); strings.TrimSpace(secret) != "" {
			item.APIKey = secret
		}
		legacy = append(legacy, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// providerKey → provider ID for dedup within this migration pass.
	byKey := map[string]string{}
	// Also load existing providers so re-runs stay stable.
	existing, err := s.ListLLMProviders(ctx)
	if err != nil {
		return 0, err
	}
	for _, provider := range existing {
		key := providerDedupeKey(provider.Driver, provider.BaseURL, provider.APIKey)
		byKey[key] = provider.ID
	}

	moved := 0
	for _, item := range legacy {
		if strings.TrimSpace(item.ProviderID) != "" {
			// Already linked; still ensure secret lives on the provider.
			if secret := strings.TrimSpace(item.APIKey); secret != "" {
				if current := s.providerSecret(item.ProviderID); strings.TrimSpace(current) == "" {
					if err := s.saveProviderSecret(item.ProviderID, secret); err != nil {
						return moved, err
					}
				}
				_ = s.deleteProfileSecret(item.ID)
			}
			continue
		}

		key := providerDedupeKey(item.Driver, item.BaseURL, item.APIKey)
		providerID, ok := byKey[key]
		if !ok {
			providerID = newID("llmp")
			name := providerNameFromLegacy(item.Name, item.Driver, item.BaseURL)
			// Ensure unique name.
			name = s.uniqueProviderName(ctx, name)
			provider := LLMProvider{
				ID:        providerID,
				Name:      name,
				Driver:    item.Driver,
				BaseURL:   strings.TrimRight(strings.TrimSpace(item.BaseURL), "/"),
				APIKey:    item.APIKey,
				CreatedAt: item.CreatedAt,
				UpdatedAt: item.UpdatedAt,
			}
			if provider.Driver == "" {
				provider.Driver = "openai-compatible"
			}
			if _, err := s.SaveLLMProvider(ctx, provider); err != nil {
				return moved, fmt.Errorf("create provider for profile %s: %w", item.ID, err)
			}
			byKey[key] = providerID
		} else if secret := strings.TrimSpace(item.APIKey); secret != "" {
			if current := s.providerSecret(providerID); strings.TrimSpace(current) == "" {
				if err := s.saveProviderSecret(providerID, secret); err != nil {
					return moved, err
				}
			}
		}

		if _, err := s.db.ExecContext(ctx, `
			UPDATE recognizer_profiles
			SET provider_id = ?, base_url = '', api_key = '', driver = ?, updated_at = ?
			WHERE id = ?
		`, providerID, item.Driver, now(), item.ID); err != nil {
			return moved, err
		}
		_ = s.deleteProfileSecret(item.ID)
		moved++
	}
	return moved, nil
}

func providerDedupeKey(driver, baseURL, apiKey string) string {
	return strings.ToLower(strings.TrimSpace(driver)) + "\n" +
		strings.TrimRight(strings.TrimSpace(baseURL), "/") + "\n" +
		strings.TrimSpace(apiKey)
}

func providerNameFromLegacy(profileName, driver, baseURL string) string {
	profileName = strings.TrimSpace(profileName)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if driver == "mock" {
		return "Mock"
	}
	if baseURL != "" {
		// Prefer host-ish label from URL.
		trimmed := strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
		if idx := strings.Index(trimmed, "/"); idx > 0 {
			trimmed = trimmed[:idx]
		}
		if trimmed != "" {
			return trimmed
		}
	}
	if profileName != "" {
		return profileName + " 接口"
	}
	return "默认接口"
}

func (s *Store) uniqueProviderName(ctx context.Context, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "接口"
	}
	name := base
	for i := 2; ; i++ {
		var existing string
		err := s.db.QueryRowContext(ctx, `SELECT id FROM llm_providers WHERE name = ? COLLATE NOCASE`, name).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return name
		}
		if err != nil {
			return fmt.Sprintf("%s-%d", base, i)
		}
		name = fmt.Sprintf("%s (%d)", base, i)
	}
}
