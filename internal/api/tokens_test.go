package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
)

func postJSON(srv *Server, path, token string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestCreateTokenReturnsThePlaintextOnceAndNeverAgain(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	root := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/tokens", root, map[string]any{
		"name":   "ci",
		"scopes": []string{"exec", "sandboxes:write"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created apigen.CreatedToken
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(created.Token, "orcal_") {
		t.Fatalf("expected a plaintext token, got %q", created.Token)
	}
	if created.Prefix != created.Token[:12] {
		t.Fatal("the returned prefix must match the plaintext")
	}

	listRec := doRequest(srv, http.MethodGet, "/v1/tokens", root)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d", listRec.Code)
	}
	if strings.Contains(listRec.Body.String(), created.Token) {
		t.Fatal("the plaintext must never appear in a list response")
	}
	if !strings.Contains(listRec.Body.String(), created.Prefix) {
		t.Fatal("the prefix must appear so an operator can identify the token")
	}
}

func TestCreateTokenRejectsEscalation(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	if _, _, err := svc.Create(context.Background(),
		auth.CreateOptions{Name: "root", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll}); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	adminOnly := mint(t, svc, "ops", auth.Scopes{auth.ScopeAdmin})

	rec := postJSON(srv, "/v1/tokens", adminOnly, map[string]any{
		"name":   "sneaky",
		"scopes": []string{"sandboxes:write"},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sandboxes:write") {
		t.Errorf("the refusal must name the offending scope, got %s", rec.Body.String())
	}
}

func TestCreateTokenValidation(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	root := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"unknown scope", map[string]any{"name": "x", "scopes": []string{"sandboxes:delete"}}, http.StatusBadRequest},
		{"empty scopes", map[string]any{"name": "x", "scopes": []string{}}, http.StatusBadRequest},
		{"missing scopes", map[string]any{"name": "x"}, http.StatusBadRequest},
		{"bad name", map[string]any{"name": "NOPE", "scopes": []string{"exec"}}, http.StatusBadRequest},
		{"duplicate name", map[string]any{"name": "root", "scopes": []string{"exec"}}, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(srv, "/v1/tokens", root, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRevokeToken(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	root := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	victim := mint(t, svc, "ci", auth.Scopes{auth.ScopeExec})

	tokens, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var victimID string
	for _, tok := range tokens {
		if tok.Name == "ci" {
			victimID = tok.ID
		}
	}

	rec := doRequest(srv, http.MethodDelete, "/v1/tokens/"+victimID, root)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if again := doRequest(srv, http.MethodDelete, "/v1/tokens/"+victimID, root); again.Code != http.StatusNoContent {
		t.Fatalf("revocation must be idempotent, got %d", again.Code)
	}
	if rec := doRequest(srv, http.MethodGet, "/v1/sandboxes", victim); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked token must stop working immediately, got %d", rec.Code)
	}
	if missing := doRequest(srv, http.MethodDelete, "/v1/tokens/nope", root); missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown id, got %d", missing.Code)
	}
}

func TestRevokingTheLastAdminIsRefused(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	root := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	tokens, _ := svc.List(context.Background())
	rec := doRequest(srv, http.MethodDelete, "/v1/tokens/"+tokens[0].ID, root)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestATokenMayRevokeItself(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	if _, _, err := svc.Create(context.Background(),
		auth.CreateOptions{Name: "root", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll}); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	self := mint(t, svc, "leaked", auth.Scopes{auth.ScopeAdmin})
	tokens, _ := svc.List(context.Background())
	var selfID string
	for _, tok := range tokens {
		if tok.Name == "leaked" {
			selfID = tok.ID
		}
	}
	if rec := doRequest(srv, http.MethodDelete, "/v1/tokens/"+selfID, self); rec.Code != http.StatusNoContent {
		t.Fatalf("a token suspecting it leaked must be able to revoke itself, got %d", rec.Code)
	}
}

func TestTokenRoutesRequireAdmin(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	notAdmin := mint(t, svc, "ci", auth.Scopes{auth.ScopeSandboxesWrite, auth.ScopeExec, auth.ScopeAuditRead})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/tokens"},
		{http.MethodPost, "/v1/tokens"},
		{http.MethodDelete, "/v1/tokens/anything"},
	} {
		rec := doRequest(srv, tc.method, tc.path, notAdmin)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s must require admin, got %d", tc.method, tc.path, rec.Code)
		}
	}
}
