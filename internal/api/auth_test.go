package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/auth"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newAuthTestServer(t *testing.T) (*Server, *auth.Service) {
	t.Helper()
	repo := auth.NewMemoryRepo()
	svc := auth.NewService(repo)
	srv := NewServer(Options{Tokens: svc, Version: "test", Logger: testLogger()})
	return srv, svc
}

func mint(t *testing.T, svc *auth.Service, name string, scopes auth.Scopes) string {
	t.Helper()
	_, plaintext, err := svc.Create(context.Background(), auth.CreateOptions{Name: name, Scopes: scopes}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("mint %s: %v", name, err)
	}
	return plaintext
}

func doRequest(srv *Server, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPublicRoutesNeedNoToken(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	for _, path := range []string{"/v1/healthz", "/v1/version"} {
		if rec := doRequest(srv, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("%s must be public, got %d", path, rec.Code)
		}
	}
}

func TestEveryUnauthorizedCauseReturnsTheIdenticalBody(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	ctx := context.Background()

	expired, plaintextExpired, err := svc.Create(ctx, auth.CreateOptions{Name: "expired", Scopes: auth.Scopes{auth.ScopeSandboxesRead}, ExpiresIn: 1}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	_ = expired

	revokedPlaintext := mint(t, svc, "revoked", auth.Scopes{auth.ScopeSandboxesRead})
	tokens, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, tok := range tokens {
		if tok.Name == "revoked" {
			if err := svc.Revoke(ctx, tok.ID); err != nil {
				t.Fatalf("revoke: %v", err)
			}
		}
	}

	cases := map[string]string{
		"missing": "",
		"unknown": "orcal_definitely-not-a-real-token-value-here",
		"expired": plaintextExpired,
		"revoked": revokedPlaintext,
	}

	var bodies []string
	for name, token := range cases {
		rec := doRequest(srv, http.MethodGet, "/v1/sandboxes", token)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", name, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("%s: expected a Bearer challenge, got %q", name, got)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("401 bodies must be byte-identical so a caller cannot tell why:\n%q\n%q", bodies[0], bodies[i])
		}
	}
}

func TestMalformedAuthorizationHeaderIsUnauthorized(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	good := mint(t, svc, "ci", auth.Scopes{auth.ScopeSandboxesRead})

	for _, header := range []string{good, "Basic " + good, "Bearer", "Bearer  ", "bearer"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q must be rejected, got %d", header, rec.Code)
		}
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	token := mint(t, svc, "ci", auth.Scopes{auth.ScopeSandboxesRead})
	req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("RFC 7235 makes the scheme case-insensitive")
	}
}

func TestWrongScopeIsForbiddenNotUnauthorized(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	token := mint(t, svc, "reader", auth.Scopes{auth.ScopeSandboxesRead})

	if rec := doRequest(srv, http.MethodGet, "/v1/sandboxes", token); rec.Code == http.StatusForbidden {
		t.Fatal("sandboxes:read must be allowed to list sandboxes")
	}

	rec := doRequest(srv, http.MethodPost, "/v1/sandboxes", token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Error("403 must not carry a challenge; re-presenting the same credential cannot help")
	}
	if body := rec.Body.String(); !strings.Contains(body, "sandboxes:write") {
		t.Errorf("403 must name the required scope, got %s", body)
	}
}

func TestWildcardTokenReachesEveryScopedRoute(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	for _, r := range (&Server{}).routes() {
		if r.Public {
			continue
		}
		rec := doRequest(srv, r.Method, staticPath(r.Path), token)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s %s rejected a wildcard token with %d", r.Method, r.Path, rec.Code)
		}
	}
}

func TestEveryScopedRouteRejectsAScopelessToken(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	token := mint(t, svc, "narrow", auth.Scopes{auth.ScopeAuditRead})
	for _, r := range (&Server{}).routes() {
		if r.Public || r.Scope == auth.ScopeAuditRead {
			continue
		}
		rec := doRequest(srv, r.Method, staticPath(r.Path), token)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s must reject a token holding only audit:read, got %d", r.Method, r.Path, rec.Code)
		}
	}
}

func staticPath(pattern string) string {
	out := strings.ReplaceAll(pattern, "{ref}", "does-not-exist")
	return strings.ReplaceAll(out, "{id}", "does-not-exist")
}
