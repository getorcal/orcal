package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultURL = "http://127.0.0.1:8080"

type settings struct {
	URL    string
	Token  string
	Output string
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "orcal", "config.yaml")
}

func resolveSettings(flagURL, flagToken, flagOutput, configPath string) (settings, error) {
	s := settings{URL: defaultURL, Output: "human"}

	explicitConfig := configPath != ""
	if !explicitConfig {
		configPath = defaultConfigPath()
	}
	if configPath != "" {
		fileURL, fileToken, err := readConfigFile(configPath)
		if err != nil {
			if explicitConfig {
				return settings{}, fmt.Errorf("read config file %s: %w", configPath, err)
			}
		} else {
			if fileURL != "" {
				s.URL = fileURL
			}
			if fileToken != "" {
				s.Token = fileToken
			}
		}
	}

	if v := os.Getenv("ORCAL_URL"); v != "" {
		s.URL = v
	}
	if v := os.Getenv("ORCAL_TOKEN"); v != "" {
		s.Token = v
	}

	if flagURL != "" {
		s.URL = flagURL
	}
	if flagToken != "" {
		s.Token = flagToken
	}
	if flagOutput != "" {
		s.Output = flagOutput
	}

	if s.Output != "human" && s.Output != "json" {
		return settings{}, fmt.Errorf("unsupported output format %q: use human or json", s.Output)
	}
	return s, nil
}

func readConfigFile(path string) (string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var url, token string
	for line := range strings.SplitSeq(string(raw), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "url":
			url = value
		case "token":
			token = value
		}
	}
	return url, token, nil
}
