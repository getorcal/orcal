package config

import "testing"

func TestLoadUsesDefaultsWhenEnvUnset(t *testing.T) {
	t.Setenv("ORCAL_ADDR", "")
	t.Setenv("ORCAL_DATA_DIR", "")
	t.Setenv("ORCAL_EXEC_OUTPUT_MAX_BYTES", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want 127.0.0.1:8080", c.Addr)
	}
	if c.DataDir != "/var/lib/orcal" {
		t.Errorf("DataDir = %q, want /var/lib/orcal", c.DataDir)
	}
	if c.ExecOutputMaxBytes != 67108864 {
		t.Errorf("ExecOutputMaxBytes = %d, want 67108864", c.ExecOutputMaxBytes)
	}
	if c.DefaultCPUMillis != 1000 {
		t.Errorf("DefaultCPUMillis = %d, want 1000", c.DefaultCPUMillis)
	}
	if c.DefaultMemoryBytes != 1073741824 {
		t.Errorf("DefaultMemoryBytes = %d, want 1073741824", c.DefaultMemoryBytes)
	}
	if c.DefaultPidsLimit != 512 {
		t.Errorf("DefaultPidsLimit = %d, want 512", c.DefaultPidsLimit)
	}
	if c.NetworkName != "orcal" {
		t.Errorf("NetworkName = %q, want orcal", c.NetworkName)
	}
}

func TestLoadReadsEnvOverrides(t *testing.T) {
	t.Setenv("ORCAL_ADDR", "0.0.0.0:9000")
	t.Setenv("ORCAL_DATA_DIR", "/tmp/orcal")
	t.Setenv("ORCAL_EXEC_OUTPUT_MAX_BYTES", "1024")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Addr != "0.0.0.0:9000" {
		t.Errorf("Addr = %q, want 0.0.0.0:9000", c.Addr)
	}
	if c.DataDir != "/tmp/orcal" {
		t.Errorf("DataDir = %q, want /tmp/orcal", c.DataDir)
	}
	if c.ExecOutputMaxBytes != 1024 {
		t.Errorf("ExecOutputMaxBytes = %d, want 1024", c.ExecOutputMaxBytes)
	}
}

func TestLoadRejectsNonNumericByteLimit(t *testing.T) {
	t.Setenv("ORCAL_EXEC_OUTPUT_MAX_BYTES", "big")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for non-numeric limit")
	}
}

func TestLoadRejectsAZeroOutputByteLimit(t *testing.T) {
	t.Setenv("ORCAL_EXEC_OUTPUT_MAX_BYTES", "0")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error - a zero limit captures no output at all")
	}
}

func TestLoadRejectsNonPositiveNumericSettings(t *testing.T) {
	keys := []string{
		"ORCAL_EXEC_OUTPUT_MAX_BYTES",
		"ORCAL_DEFAULT_CPU_MILLIS",
		"ORCAL_DEFAULT_MEMORY_BYTES",
		"ORCAL_DEFAULT_PIDS_LIMIT",
	}
	for _, key := range keys {
		for _, value := range []string{"0", "-1"} {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Setenv(key, value)

				if _, err := Load(); err == nil {
					t.Fatalf("Load() error = nil, want an error for %s=%s", key, value)
				}
			})
		}
	}
}

func TestLoadFileLimitDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FileMaxBytes != 64<<20 {
		t.Errorf("FileMaxBytes = %d, want %d", cfg.FileMaxBytes, 64<<20)
	}
	if cfg.ArchiveMaxBytes != 1<<30 {
		t.Errorf("ArchiveMaxBytes = %d, want %d", cfg.ArchiveMaxBytes, 1<<30)
	}
	if cfg.ListMaxEntries != 10000 {
		t.Errorf("ListMaxEntries = %d, want 10000", cfg.ListMaxEntries)
	}
	if cfg.ListMaxScanBytes != 256<<20 {
		t.Errorf("ListMaxScanBytes = %d, want %d", cfg.ListMaxScanBytes, 256<<20)
	}
}

func TestLoadFileLimitOverrides(t *testing.T) {
	t.Setenv("ORCAL_FILE_MAX_BYTES", "1024")
	t.Setenv("ORCAL_ARCHIVE_MAX_BYTES", "2048")
	t.Setenv("ORCAL_LIST_MAX_ENTRIES", "7")
	t.Setenv("ORCAL_LIST_MAX_SCAN_BYTES", "4096")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FileMaxBytes != 1024 || cfg.ArchiveMaxBytes != 2048 {
		t.Errorf("byte limits = %d/%d, want 1024/2048", cfg.FileMaxBytes, cfg.ArchiveMaxBytes)
	}
	if cfg.ListMaxEntries != 7 || cfg.ListMaxScanBytes != 4096 {
		t.Errorf("list limits = %d/%d, want 7/4096", cfg.ListMaxEntries, cfg.ListMaxScanBytes)
	}
}

func TestLoadRejectsNegativeFileLimits(t *testing.T) {
	t.Setenv("ORCAL_FILE_MAX_BYTES", "-1")
	if _, err := Load(); err == nil {
		t.Error("Load() error = nil, want a validation error for a negative limit")
	}
}
