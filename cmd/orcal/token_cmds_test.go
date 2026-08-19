package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
		"90d": 90 * 24 * time.Hour,
		"1d":  24 * time.Hour,
		"":    0,
	}
	for input, want := range cases {
		got, err := parseDuration(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if got != want {
			t.Fatalf("%q: expected %v, got %v", input, want, got)
		}
	}
	for _, bad := range []string{"d", "-5d", "1w", "abc", "1.5d"} {
		if _, err := parseDuration(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestRenderCreatedTokenPutsOnlyThePlaintextOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	created := sampleCreatedToken()

	if err := renderCreatedToken(&stdout, &stderr, "human", created); err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := stdout.String(); got != "orcal_secretvalue\n" {
		t.Fatalf("stdout must be the plaintext alone, got %q", got)
	}
	if !strings.Contains(stderr.String(), "tok-1") {
		t.Fatalf("the id belongs on stderr, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "orcal_secretvalue") {
		t.Fatal("the plaintext must not be duplicated onto stderr")
	}
}

func TestRenderCreatedTokenJSONGoesEntirelyToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := renderCreatedToken(&stdout, &stderr, "json", sampleCreatedToken()); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("json mode must write nothing to stderr, got %q", stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout must be one JSON document: %v", err)
	}
	if decoded["token"] != "orcal_secretvalue" {
		t.Fatalf("the plaintext must be present in json mode, got %v", decoded["token"])
	}
}

func TestRenderTokenListShowsUnknownAndRevoked(t *testing.T) {
	var out bytes.Buffer
	if err := renderTokenList(&out, "human", sampleTokenList()); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "(unknown)") {
		t.Fatal("a migrated token with no prefix must render as (unknown)")
	}
	if !strings.Contains(body, "(revoked)") {
		t.Fatal("a revoked token must be marked")
	}
}

func TestCLITokenRevokeHumanAndJSON(t *testing.T) {
	env := newCLIEnv(t)
	env.run(t, "create", "--name", "my-agent", "--image", "alpine:3.20")

	stdout, stderr, code := env.run(t, "token", "create", "--name", "test-token", "--scope", "sandboxes:read", "--output", "json")
	if code != 0 {
		t.Fatalf("create exit = %d, stderr = %s", code, stderr)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	tokenID := created["id"].(string)

	t.Run("human mode emits id alone", func(t *testing.T) {
		out, _, code := env.run(t, "token", "revoke", tokenID)
		if code != 0 {
			t.Fatalf("revoke exit = %d", code)
		}
		if out != tokenID+"\n" {
			t.Fatalf("human mode must print id alone, got %q", out)
		}
	})

	stdout2, stderr2, code := env.run(t, "token", "create", "--name", "test-token-2", "--scope", "sandboxes:read", "--output", "json")
	if code != 0 {
		t.Fatalf("create exit = %d, stderr = %s", code, stderr2)
	}
	var created2 map[string]any
	if err := json.Unmarshal([]byte(stdout2), &created2); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	tokenID2 := created2["id"].(string)

	t.Run("json mode emits document", func(t *testing.T) {
		out, _, code := env.run(t, "token", "revoke", tokenID2, "--output", "json")
		if code != 0 {
			t.Fatalf("revoke exit = %d", code)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("stdout must be valid JSON: %v", err)
		}
		if decoded["id"] != tokenID2 {
			t.Fatalf("json must carry id, got %v", decoded)
		}
	})
}

func sampleCreatedToken() *apigen.CreatedToken {
	return &apigen.CreatedToken{
		Token:     "orcal_secretvalue",
		Id:        "tok-1",
		Name:      "test-token",
		Prefix:    "orcal_abc",
		CreatedAt: time.Now(),
		Scopes: []apigen.Scope{
			"api:sandbox:read",
			"api:sandbox:write",
		},
	}
}

func sampleTokenList() *apigen.TokenList {
	now := time.Now()
	return &apigen.TokenList{
		Items: []apigen.Token{
			{
				Id:        "tok-2",
				Name:      "no-prefix-token",
				Prefix:    "",
				CreatedAt: now.Add(-24 * time.Hour),
				Scopes: []apigen.Scope{
					"api:sandbox:read",
				},
			},
			{
				Id:        "tok-3",
				Name:      "revoked-token",
				Prefix:    "orcal_def",
				CreatedAt: now.Add(-48 * time.Hour),
				RevokedAt: &now,
				Scopes: []apigen.Scope{
					"api:sandbox:write",
				},
			},
		},
	}
}
