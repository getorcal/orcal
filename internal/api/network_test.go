package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

func newSandboxTestServer(t *testing.T) (*Server, *auth.Service) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	sandboxes := sandbox.NewService(st.Sandboxes(), f, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"})

	tokens := auth.NewService(auth.NewMemoryRepo())

	srv := NewServer(Options{
		Sandboxes: sandboxes,
		Tokens:    tokens,
		Audit:     audit.NewService(audit.NewMemoryRepo(), audit.RetentionPolicy{}),
		Version:   "test",
		Logger:    testLogger(),
	})
	return srv, tokens
}

func TestCreateSandboxAcceptsNetworkNone(t *testing.T) {
	srv, svc := newSandboxTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine", "network": "none"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created apigen.Sandbox
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Network != apigen.None {
		t.Fatalf("expected none, got %q", created.Network)
	}
}

func TestCreateSandboxDefaultsToFull(t *testing.T) {
	srv, svc := newSandboxTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created apigen.Sandbox
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Network != apigen.Full {
		t.Fatalf("expected full, got %q", created.Network)
	}
}

func TestCreateSandboxRejectsAnUnknownNetwork(t *testing.T) {
	srv, svc := newSandboxTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine", "network": "host"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
