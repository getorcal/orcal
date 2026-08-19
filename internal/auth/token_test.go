package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateTokenShape(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !strings.HasPrefix(token, "orcal_") {
			t.Fatalf("token must carry the orcal_ tag, got %q", token)
		}
		if len(token) != 49 {
			t.Fatalf("expected 49 characters, got %d in %q", len(token), token)
		}
		if seen[token] {
			t.Fatal("GenerateToken returned a duplicate")
		}
		seen[token] = true
	}
}

func TestPrefixOf(t *testing.T) {
	if got := PrefixOf("orcal_abcdefghij"); got != "orcal_abcdef" {
		t.Fatalf("expected orcal_abcdef, got %q", got)
	}
	if got := PrefixOf("short"); got != "short" {
		t.Fatalf("a short string must be returned whole, got %q", got)
	}
	if got := PrefixOf(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestHashTokenIsStable(t *testing.T) {
	first := HashToken("abc")
	second := HashToken("abc")
	if first != second {
		t.Fatal("hashing must be deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("different inputs must hash differently")
	}
	if len(HashToken("abc")) != 64 {
		t.Fatal("expected a 64-character hex sha256")
	}
}

func TestTokenLive(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	if !(&Token{}).Live(now) {
		t.Fatal("a token with no expiry and no revocation is live")
	}
	if (&Token{RevokedAt: &past}).Live(now) {
		t.Fatal("a revoked token is not live")
	}
	if (&Token{ExpiresAt: &past}).Live(now) {
		t.Fatal("an expired token is not live")
	}
	if !(&Token{ExpiresAt: &future}).Live(now) {
		t.Fatal("a token expiring later is live")
	}
	if (&Token{ExpiresAt: &now}).Live(now) {
		t.Fatal("expiry is exclusive: a token expiring exactly now is not live")
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"ci", "build-bot", "a", "agent-7"} {
		if err := ValidateName(ok); err != nil {
			t.Fatalf("%q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "UPPER", "has space", "trailing-", "-leading", strings.Repeat("a", 64)} {
		if err := ValidateName(bad); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("%q must be rejected, got %v", bad, err)
		}
	}
}
