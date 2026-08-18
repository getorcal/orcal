package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsPrecedenceFlagBeatsEnvBeatsFileBeatsDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("url: http://from-file:8080\ntoken: file-token\n"), 0o600)

	t.Run("default", func(t *testing.T) {
		got, err := resolveSettings("", "", "", "")
		if err != nil {
			t.Fatalf("resolveSettings() error = %v", err)
		}
		if got.URL != "http://127.0.0.1:8080" {
			t.Errorf("URL = %q, want the default", got.URL)
		}
	})

	t.Run("file beats default", func(t *testing.T) {
		got, _ := resolveSettings("", "", "", configPath)
		if got.URL != "http://from-file:8080" || got.Token != "file-token" {
			t.Errorf("settings = %+v, want the file values", got)
		}
	})

	t.Run("env beats file", func(t *testing.T) {
		t.Setenv("ORCAL_URL", "http://from-env:8080")
		t.Setenv("ORCAL_TOKEN", "env-token")
		got, _ := resolveSettings("", "", "", configPath)
		if got.URL != "http://from-env:8080" || got.Token != "env-token" {
			t.Errorf("settings = %+v, want the env values", got)
		}
	})

	t.Run("flag beats env", func(t *testing.T) {
		t.Setenv("ORCAL_URL", "http://from-env:8080")
		got, _ := resolveSettings("http://from-flag:8080", "flag-token", "", configPath)
		if got.URL != "http://from-flag:8080" || got.Token != "flag-token" {
			t.Errorf("settings = %+v, want the flag values", got)
		}
	})
}

func TestResolveSettingsRejectsUnknownOutputFormat(t *testing.T) {
	if _, err := resolveSettings("", "", "yaml", ""); err == nil {
		t.Fatal("resolveSettings() error = nil, want an error for an unsupported format")
	}
}

func TestResolveSettingsErrorsOnUnreadableExplicitConfigPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := resolveSettings("", "", "", missing)

	if err == nil {
		t.Fatal("resolveSettings() error = nil, want an error for an unreadable explicit config path")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name the path %q", err, missing)
	}
}

func TestResolveSettingsIgnoresAMissingDefaultConfigPath(t *testing.T) {
	got, err := resolveSettings("", "", "", "")

	if err != nil {
		t.Fatalf("resolveSettings() error = %v, want a missing default config path to be ignored", err)
	}
	if got.URL != "http://127.0.0.1:8080" {
		t.Errorf("URL = %q, want the default", got.URL)
	}
}
