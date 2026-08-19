package auth

import (
	"fmt"
	"slices"
)

type Scope string

const (
	ScopeSandboxesRead  Scope = "sandboxes:read"
	ScopeSandboxesWrite Scope = "sandboxes:write"
	ScopeExec           Scope = "exec"
	ScopeFilesRead      Scope = "files:read"
	ScopeFilesWrite     Scope = "files:write"
	ScopeSnapshotsRead  Scope = "snapshots:read"
	ScopeSnapshotsWrite Scope = "snapshots:write"
	ScopeAuditRead      Scope = "audit:read"
	ScopeAdmin          Scope = "admin"

	ScopeAll Scope = "*"
)

var knownScopes = []Scope{
	ScopeSandboxesRead,
	ScopeSandboxesWrite,
	ScopeExec,
	ScopeFilesRead,
	ScopeFilesWrite,
	ScopeSnapshotsRead,
	ScopeSnapshotsWrite,
	ScopeAuditRead,
	ScopeAdmin,
}

func KnownScopes() Scopes {
	out := make(Scopes, len(knownScopes))
	copy(out, knownScopes)
	return out
}

func validScope(s Scope) bool {
	if s == ScopeAll {
		return true
	}
	return slices.Contains(knownScopes, s)
}

type Scopes []Scope

func (s Scopes) Has(want Scope) bool {
	for _, held := range s {
		if held == ScopeAll || held == want {
			return true
		}
	}
	return false
}

func (s Scopes) Covers(want Scopes) bool {
	return len(s.Missing(want)) == 0
}

func (s Scopes) Missing(want Scopes) Scopes {
	var out Scopes
	for _, w := range want {
		if !s.Has(w) {
			out = append(out, w)
		}
	}
	return out
}

func (s Scopes) AdminCapable() bool {
	for _, held := range s {
		if held == ScopeAdmin || held == ScopeAll {
			return true
		}
	}
	return false
}

func ValidateScopes(s Scopes) error {
	if len(s) == 0 {
		return fmt.Errorf("%w: at least one scope is required", ErrInvalidScope)
	}
	for _, scope := range s {
		if !validScope(scope) {
			return fmt.Errorf("%w: %q", ErrInvalidScope, scope)
		}
	}
	return nil
}
