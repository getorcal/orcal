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
