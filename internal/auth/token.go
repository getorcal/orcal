package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const SettingKey = "auth_token_hash"

type SettingsStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Verify(hash, token string) bool {
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(HashToken(token))) == 1
}

func Ensure(ctx context.Context, store SettingsStore, configured string) (string, bool, error) {
	if configured != "" {
		if err := store.Set(ctx, SettingKey, HashToken(configured)); err != nil {
			return "", false, err
		}
		return configured, false, nil
	}

	_, found, err := store.Get(ctx, SettingKey)
	if err != nil {
		return "", false, err
	}
	if found {
		return "", false, nil
	}

	token, err := GenerateToken()
	if err != nil {
		return "", false, err
	}
	if err := store.Set(ctx, SettingKey, HashToken(token)); err != nil {
		return "", false, err
	}
	return token, true, nil
}
