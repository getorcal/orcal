package auth

import (
	"errors"
	"testing"
)

func TestHasIsExactUnlessWildcard(t *testing.T) {
	s := Scopes{ScopeSandboxesRead}
	if !s.Has(ScopeSandboxesRead) {
		t.Fatal("expected the held scope to be granted")
	}
	if s.Has(ScopeSandboxesWrite) {
		t.Fatal("sandboxes:read must not grant sandboxes:write")
	}
	if s.Has(ScopeAdmin) {
		t.Fatal("sandboxes:read must not grant admin")
	}
}

func TestAdminDoesNotImplyAnythingElse(t *testing.T) {
	s := Scopes{ScopeAdmin}
	for _, other := range KnownScopes() {
		if other == ScopeAdmin {
			continue
		}
		if s.Has(other) {
			t.Fatalf("admin must not imply %s", other)
		}
	}
}

func TestWildcardGrantsEveryKnownScope(t *testing.T) {
	s := Scopes{ScopeAll}
	for _, other := range KnownScopes() {
		if !s.Has(other) {
			t.Fatalf("wildcard must grant %s", other)
		}
	}
	if !s.Has(ScopeAll) {
		t.Fatal("wildcard must grant itself")
	}
}

func TestCoversRejectsEscalation(t *testing.T) {
	admin := Scopes{ScopeAdmin}
	if admin.Covers(Scopes{ScopeSandboxesWrite}) {
		t.Fatal("admin alone must not be able to grant sandboxes:write")
	}
	if admin.Covers(Scopes{ScopeAll}) {
		t.Fatal("admin alone must not be able to grant the wildcard")
	}
	if !admin.Covers(Scopes{ScopeAdmin}) {
		t.Fatal("a token must be able to grant a scope it holds")
	}
	if !(Scopes{ScopeAll}).Covers(Scopes{ScopeSandboxesWrite, ScopeAdmin}) {
		t.Fatal("wildcard must be able to grant anything")
	}
}

func TestMissingNamesTheOffendingScopes(t *testing.T) {
	got := Scopes{ScopeAdmin}.Missing(Scopes{ScopeAdmin, ScopeExec, ScopeFilesRead})
	if len(got) != 2 || got[0] != ScopeExec || got[1] != ScopeFilesRead {
		t.Fatalf("expected [exec files:read], got %v", got)
	}
}

func TestAdminCapable(t *testing.T) {
	for _, s := range []Scopes{{ScopeAdmin}, {ScopeAll}, {ScopeExec, ScopeAdmin}} {
		if !s.AdminCapable() {
			t.Fatalf("%v must be admin-capable", s)
		}
	}
	for _, s := range []Scopes{{ScopeExec}, {}, {ScopeSandboxesWrite, ScopeAuditRead}} {
		if s.AdminCapable() {
			t.Fatalf("%v must not be admin-capable", s)
		}
	}
}

func TestValidateScopes(t *testing.T) {
	if err := ValidateScopes(Scopes{ScopeExec, ScopeAll}); err != nil {
		t.Fatalf("known scopes must validate: %v", err)
	}
	if err := ValidateScopes(nil); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("an empty scope set must be rejected, got %v", err)
	}
	if err := ValidateScopes(Scopes{"sandboxes:delete"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("an unknown scope must be rejected, got %v", err)
	}
	if err := ValidateScopes(Scopes{"SANDBOXES:READ"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("scope matching must be case-sensitive, got %v", err)
	}
}

func TestKnownScopesHasExactlyNineAndExcludesWildcard(t *testing.T) {
	known := KnownScopes()
	if len(known) != 9 {
		t.Fatalf("expected 9 known scopes, got %d: %v", len(known), known)
	}
	for _, s := range known {
		if s == ScopeAll {
			t.Fatal("KnownScopes must not include the wildcard")
		}
	}
}
