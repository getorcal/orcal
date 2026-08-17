package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrSettingNotFound = errors.New("sqlite: setting not found")

type SettingsStore struct {
	db *sql.DB
}

func (s *SettingsStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrSettingNotFound, key)
	}
	if err != nil {
		return "", fmt.Errorf("sqlite: get setting: %w", err)
	}
	return value, nil
}

func (s *SettingsStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("sqlite: set setting: %w", err)
	}
	return nil
}
