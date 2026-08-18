package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Addr               string
	DataDir            string
	Token              string
	DockerHost         string
	ExecOutputMaxBytes int64
	DefaultCPUMillis   int
	DefaultMemoryBytes int64
	DefaultPidsLimit   int
	NetworkName        string
}

func Load() (Config, error) {
	c := Config{
		Addr:        envString("ORCAL_ADDR", "127.0.0.1:8080"),
		DataDir:     envString("ORCAL_DATA_DIR", "/var/lib/orcal"),
		Token:       envString("ORCAL_TOKEN", ""),
		DockerHost:  envString("ORCAL_DOCKER_HOST", ""),
		NetworkName: envString("ORCAL_NETWORK_NAME", "orcal"),
	}

	var err error
	if c.ExecOutputMaxBytes, err = envPositiveInt64("ORCAL_EXEC_OUTPUT_MAX_BYTES", 67108864); err != nil {
		return Config{}, err
	}
	if c.DefaultMemoryBytes, err = envPositiveInt64("ORCAL_DEFAULT_MEMORY_BYTES", 1073741824); err != nil {
		return Config{}, err
	}
	cpu, err := envPositiveInt64("ORCAL_DEFAULT_CPU_MILLIS", 1000)
	if err != nil {
		return Config{}, err
	}
	c.DefaultCPUMillis = int(cpu)
	pids, err := envPositiveInt64("ORCAL_DEFAULT_PIDS_LIMIT", 512)
	if err != nil {
		return Config{}, err
	}
	c.DefaultPidsLimit = int(pids)

	return c, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt64(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer: %w", key, err)
	}
	return n, nil
}

func envPositiveInt64(key string, fallback int64) (int64, error) {
	n, err := envInt64(key, fallback)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("config: %s must be greater than zero, got %d", key, n)
	}
	return n, nil
}
