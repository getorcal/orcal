package config

import (
	"fmt"
	"os"
	"strconv"
)

// time.Duration(days) * 24 * time.Hour overflows int64 nanoseconds above ~106751 days;
// 36500 (100 years) keeps every downstream computation well clear of that wraparound.
const maxAuditRetentionDays = 36500

type Config struct {
	Addr               string
	DataDir            string
	Token              string
	DockerHost         string
	ContainerRuntime   string
	ExecOutputMaxBytes int64
	DefaultCPUMillis   int
	DefaultMemoryBytes int64
	DefaultPidsLimit   int
	NetworkName        string
	FileMaxBytes       int64
	ArchiveMaxBytes    int64
	ListMaxEntries     int
	ListMaxScanBytes   int64
	AuditRetentionDays int
	AuditMaxEvents     int
}

func Load() (Config, error) {
	c := Config{
		Addr:             envString("ORCAL_ADDR", "127.0.0.1:8080"),
		DataDir:          envString("ORCAL_DATA_DIR", "/var/lib/orcal"),
		Token:            envString("ORCAL_TOKEN", ""),
		DockerHost:       envString("ORCAL_DOCKER_HOST", ""),
		ContainerRuntime: envString("ORCAL_CONTAINER_RUNTIME", ""),
		NetworkName:      envString("ORCAL_NETWORK_NAME", "orcal"),
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
	if c.FileMaxBytes, err = envPositiveInt64("ORCAL_FILE_MAX_BYTES", 64<<20); err != nil {
		return Config{}, err
	}
	if c.ArchiveMaxBytes, err = envPositiveInt64("ORCAL_ARCHIVE_MAX_BYTES", 1<<30); err != nil {
		return Config{}, err
	}
	listEntries, err := envPositiveInt64("ORCAL_LIST_MAX_ENTRIES", 10000)
	if err != nil {
		return Config{}, err
	}
	c.ListMaxEntries = int(listEntries)
	if c.ListMaxScanBytes, err = envPositiveInt64("ORCAL_LIST_MAX_SCAN_BYTES", 256<<20); err != nil {
		return Config{}, err
	}
	retentionDays, err := envPositiveInt64("ORCAL_AUDIT_RETENTION_DAYS", 90)
	if err != nil {
		return Config{}, err
	}
	if retentionDays > maxAuditRetentionDays {
		return Config{}, fmt.Errorf("config: ORCAL_AUDIT_RETENTION_DAYS must not exceed %d, got %d",
			maxAuditRetentionDays, retentionDays)
	}
	c.AuditRetentionDays = int(retentionDays)
	maxEvents, err := envPositiveInt64("ORCAL_AUDIT_MAX_EVENTS", 1000000)
	if err != nil {
		return Config{}, err
	}
	c.AuditMaxEvents = int(maxEvents)

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
