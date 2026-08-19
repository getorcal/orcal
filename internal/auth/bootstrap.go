package auth

import (
	"context"
	"errors"
	"fmt"
)

const BootstrapName = "bootstrap"

func (s *Service) Bootstrap(ctx context.Context, settings SettingsStore, configured string) (string, bool, error) {
	if err := s.migrateSettingsToken(ctx, settings); err != nil {
		return "", false, err
	}
	if configured != "" {
		return "", false, s.pinBootstrap(ctx, configured)
	}

	live, err := s.hasLiveToken(ctx)
	if err != nil {
		return "", false, err
	}
	if live {
		return "", false, nil
	}

	plaintext, err := GenerateToken()
	if err != nil {
		return "", false, err
	}
	now := s.now()
	tok := &Token{
		ID:        s.newID(),
		Name:      BootstrapName,
		Hash:      HashToken(plaintext),
		Prefix:    PrefixOf(plaintext),
		Scopes:    Scopes{ScopeAll},
		CreatedAt: now,
	}
	if err := s.repo.Create(ctx, tok); err != nil {
		return "", false, err
	}
	return plaintext, true, nil
}

func (s *Service) migrateSettingsToken(ctx context.Context, settings SettingsStore) error {
	hash, found, err := settings.Get(ctx, SettingKey)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	tok := &Token{
		ID:        s.newID(),
		Name:      BootstrapName,
		Hash:      hash,
		Scopes:    Scopes{ScopeAll},
		CreatedAt: s.now(),
	}
	if err := s.repo.Create(ctx, tok); err != nil && !errors.Is(err, ErrNameTaken) {
		return fmt.Errorf("auth: migrate legacy token: %w", err)
	}
	return settings.Delete(ctx, SettingKey)
}

func (s *Service) pinBootstrap(ctx context.Context, configured string) error {
	now := s.now()
	existing, err := s.repo.GetByName(ctx, BootstrapName)
	switch {
	case err == nil:
		existing.Hash = HashToken(configured)
		existing.Prefix = PrefixOf(configured)
		existing.Scopes = Scopes{ScopeAll}
		existing.ExpiresAt = nil
		return s.repo.Update(ctx, existing)
	case errors.Is(err, ErrNotFound):
		return s.repo.Create(ctx, &Token{
			ID:        s.newID(),
			Name:      BootstrapName,
			Hash:      HashToken(configured),
			Prefix:    PrefixOf(configured),
			Scopes:    Scopes{ScopeAll},
			CreatedAt: now,
		})
	default:
		return err
	}
}

func (s *Service) hasLiveToken(ctx context.Context) (bool, error) {
	tokens, err := s.repo.List(ctx)
	if err != nil {
		return false, err
	}
	now := s.now()
	for _, t := range tokens {
		if t.Live(now) {
			return true, nil
		}
	}
	return false, nil
}
