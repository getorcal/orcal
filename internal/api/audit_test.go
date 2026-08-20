package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

func newAuditTestServer(t *testing.T) (*Server, *auth.Service, *audit.Service) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	sandboxes := sandbox.NewService(st.Sandboxes(), f, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"}, "")
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, f)
	sandboxes.SetSnapshots(snapshots)

	tokens := auth.NewService(auth.NewMemoryRepo())
	events := audit.NewService(audit.NewMemoryRepo(), audit.RetentionPolicy{})

	srv := NewServer(Options{
		Sandboxes: sandboxes,
		Snapshots: snapshots,
		Tokens:    tokens,
		Audit:     events,
		Version:   "test",
		Logger:    testLogger(),
	})
	return srv, tokens, events
}

type failingAuditRepo struct{}

func (failingAuditRepo) Create(context.Context, *audit.Event) error {
	return context.DeadlineExceeded
}

func (failingAuditRepo) List(context.Context, audit.Filter) ([]*audit.Event, error) {
	return nil, nil
}

func (failingAuditRepo) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (failingAuditRepo) DeleteBeyondCount(context.Context, int) (int64, error) {
	return 0, nil
}

func newFailingAuditServer(t *testing.T) (*Server, *auth.Service) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	sandboxes := sandbox.NewService(st.Sandboxes(), f, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"}, "")

	tokens := auth.NewService(auth.NewMemoryRepo())
	events := audit.NewService(failingAuditRepo{}, audit.RetentionPolicy{})

	srv := NewServer(Options{
		Sandboxes: sandboxes,
		Tokens:    tokens,
		Audit:     events,
		Version:   "test",
		Logger:    testLogger(),
	})
	return srv, tokens
}

func TestMutationsAreAudited(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	e := got[0]
	if e.Action != audit.ActionSandboxCreate {
		t.Fatalf("expected sandbox.create, got %q", e.Action)
	}
	if e.ActorName != "root" || e.ActorTokenID == "" {
		t.Fatalf("the event must name its actor, got %+v", e)
	}
	if e.Status != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", e.Status)
	}
	if e.RequestID == "" {
		t.Fatal("the event must carry the request id")
	}
	if e.RemoteAddr == "" {
		t.Fatal("the event must carry the peer address")
	}
	if e.ResourceID == "" {
		t.Fatal("the handler must annotate the created resource id")
	}
}

func TestReadsAreNotAuditedExceptFileReads(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	if rec := doRequest(srv, http.MethodGet, "/v1/sandboxes", token); rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("listing sandboxes must leave no trail, got %d events", len(got))
	}
}

func TestUnauthorizedRequestsAreAudited(t *testing.T) {
	srv, _, events := newAuditTestServer(t)

	if rec := doRequest(srv, http.MethodGet, "/v1/sandboxes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Action != audit.ActionAuthDenied {
		t.Fatalf("expected one auth.denied event, got %+v", got)
	}
	if got[0].Status != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", got[0].Status)
	}
	if got[0].ActorTokenID != "" {
		t.Fatal("an unauthenticated request has no actor")
	}
	if got[0].Details["reason"] != "missing" {
		t.Fatalf("a request with no token must be recorded with reason missing, got %v", got[0].Details)
	}
}

func TestUnauthorizedRequestsRecordTheSpecificDenialReason(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	ctx := context.Background()

	expiredTok, plaintextExpired, err := svc.Create(ctx,
		auth.CreateOptions{Name: "expired", Scopes: auth.Scopes{auth.ScopeSandboxesRead}, ExpiresIn: 1}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}

	revokedPlaintext := mint(t, svc, "revoked", auth.Scopes{auth.ScopeSandboxesRead})
	tokens, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var revokedTok *auth.Token
	for _, tok := range tokens {
		if tok.Name == "revoked" {
			revokedTok = tok
		}
	}
	if revokedTok == nil {
		t.Fatal("could not find the minted revoked token")
	}
	if err := svc.Revoke(ctx, revokedTok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	cases := []struct {
		name       string
		header     string
		wantReason string
		wantPrefix string
	}{
		{"missing", "", "missing", ""},
		{"malformed", "Basic whatever", "malformed", ""},
		{"unknown", "Bearer orcal_definitely-not-a-real-token", "unknown", ""},
		{"expired", "Bearer " + plaintextExpired, "expired", expiredTok.Prefix},
		{"revoked", "Bearer " + revokedPlaintext, "revoked", revokedTok.Prefix},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", tc.name, rec.Code)
		}
	}

	got, err := events.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(cases) {
		t.Fatalf("expected %d events, got %d", len(cases), len(got))
	}
	byReason := make(map[string]*audit.Event, len(got))
	for _, e := range got {
		byReason[e.Details["reason"].(string)] = e
	}
	for _, tc := range cases {
		e, ok := byReason[tc.wantReason]
		if !ok {
			t.Errorf("no event recorded with reason %q", tc.wantReason)
			continue
		}
		gotPrefix, hasPrefix := e.Details["token_prefix"]
		if tc.wantPrefix == "" {
			if hasPrefix {
				t.Errorf("%s: token_prefix must be absent when the credential never resolved, got %v", tc.name, gotPrefix)
			}
			continue
		}
		if gotPrefix != tc.wantPrefix {
			t.Errorf("%s: token_prefix = %v, want %q", tc.name, gotPrefix, tc.wantPrefix)
		}
	}
}

func TestForbiddenRequestsRecordTheRequiredScope(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "reader", auth.Scopes{auth.ScopeSandboxesRead})

	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	if got[0].Action != audit.ActionAuthDenied {
		t.Fatalf("a 403 must be recorded as auth.denied, got %q", got[0].Action)
	}
	if got[0].ActorTokenID == "" {
		t.Fatal("a 403 comes from an authenticated caller and must name it")
	}
	if got[0].Details["required_scope"] != "sandboxes:write" {
		t.Fatalf("the required scope must be recorded, got %v", got[0].Details)
	}
	if got[0].Details["reason"] != "insufficient_scope" {
		t.Fatalf("the denial reason must be insufficient_scope, got %v", got[0].Details)
	}
}

func TestSandboxCreationRecordsTheNameInAuditDetails(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine", "name": "my-agent"}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	if got[0].Details["name"] != "my-agent" {
		t.Fatalf("sandbox.create must record the sandbox name, got %v", got[0].Details)
	}
	if got[0].Details["image"] != "alpine" || got[0].Details["network"] != "full" {
		t.Fatalf("sandbox.create must still record image and network, got %v", got[0].Details)
	}
}

func TestTokenCreationRecordsIDAndScopesInAuditDetails(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/tokens", token, map[string]any{"name": "ci", "scopes": []string{"exec", "sandboxes:write"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created apigen.CreatedToken
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	if got[0].Details["token_id"] != created.Id {
		t.Fatalf("token.create must record token_id, got %v", got[0].Details)
	}
	scopes, ok := got[0].Details["scopes"].([]string)
	if !ok || len(scopes) != 2 {
		t.Fatalf("token.create must record the minted scopes, got %v (%T)", got[0].Details["scopes"], got[0].Details["scopes"])
	}
}

func TestNoEventEverCarriesASecret(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{
		"image": "alpine",
		"env":   map[string]string{"AWS_SECRET_ACCESS_KEY": "super-secret-value"},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := postJSON(srv, "/v1/tokens", token, map[string]any{"name": "ci", "scopes": []string{"exec"}})
	if created.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", created.Code, created.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	plaintext, _ := body["token"].(string)

	got, err := events.List(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two events, got %d", len(got))
	}
	for _, e := range got {
		encoded, _ := json.Marshal(e)
		if strings.Contains(string(encoded), "super-secret-value") {
			t.Fatalf("an environment value leaked into %s", encoded)
		}
		if plaintext != "" && strings.Contains(string(encoded), plaintext) {
			t.Fatalf("a token plaintext leaked into %s", encoded)
		}
	}
}

func TestAuditFailureDoesNotFailTheRequest(t *testing.T) {
	srv, svc := newFailingAuditServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("a failing audit store must not fail the request, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStatusRecorderPreservesFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	if _, ok := any(wrapped).(http.Flusher); !ok {
		t.Fatal("the recorder must implement http.Flusher or SSE stops working: sse.go gives up when the assertion fails")
	}
	if _, ok := any(wrapped).(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Fatal("the recorder must implement Unwrap so http.NewResponseController reaches the real writer")
	}
}

type recordingFlusher struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *recordingFlusher) Flush() {
	f.flushes++
	f.ResponseRecorder.Flush()
}

func TestStatusRecorderFlushDelegatesToTheWrappedFlusher(t *testing.T) {
	inner := &recordingFlusher{ResponseRecorder: httptest.NewRecorder()}
	wrapped := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	wrapped.Flush()

	if inner.flushes != 1 {
		t.Fatalf("expected Flush to delegate to the wrapped flusher exactly once, got %d calls", inner.flushes)
	}
}

func TestForkedSandboxIsAuditedAsAFork(t *testing.T) {
	srv, svc, events := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	created := postJSON(srv, "/v1/sandboxes", token, map[string]any{"name": "parent", "image": "alpine"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}

	snap := postJSON(srv, "/v1/sandboxes/parent/snapshots", token, map[string]any{"name": "v1"})
	if snap.Code != http.StatusCreated {
		t.Fatalf("snapshot: %d %s", snap.Code, snap.Body.String())
	}

	forked := postJSON(srv, "/v1/sandboxes", token, map[string]any{"name": "child", "snapshot": "v1"})
	if forked.Code != http.StatusCreated {
		t.Fatalf("fork: %d %s", forked.Code, forked.Body.String())
	}

	got, err := events.List(context.Background(), audit.Filter{Action: audit.ActionSandboxFork})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one sandbox.fork event, got %d", len(got))
	}
	if got[0].Action != audit.ActionSandboxFork {
		t.Fatalf("a sandbox created from a snapshot must be recorded as sandbox.fork, not %q", got[0].Action)
	}
}
