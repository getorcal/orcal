package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

const SettingKey = "auth_token_hash"

const (
	tokenTag     = "orcal_"
	prefixLength = 12
)

type SettingsStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

type Token struct {
	ID         string
	Name       string
	Hash       string
	Prefix     string
	Scopes     Scopes
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

func (t *Token) Live(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	return t.ExpiresAt == nil || t.ExpiresAt.After(now)
}

func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return tokenTag + base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func PrefixOf(token string) string {
	if len(token) < prefixLength {
		return token
	}
	return token[:prefixLength]
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: name must match %s", ErrInvalidName, namePattern)
	}
	if name[len(name)-1] == '-' {
		return fmt.Errorf("%w: name must not end with a hyphen", ErrInvalidName)
	}
	return nil
}
