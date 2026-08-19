package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
)

func TestListEventsRequiresAuditRead(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	noAudit := mint(t, svc, "ci", auth.Scopes{auth.ScopeSandboxesWrite, auth.ScopeExec, auth.ScopeAdmin})
	if rec := doRequest(srv, http.MethodGet, "/v1/events", noAudit); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestListEventsReturnsNewestFirst(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	for range 3 {
		if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
			t.Fatalf("create: %d", rec.Code)
		}
		time.Sleep(2 * time.Millisecond)
	}

	rec := doRequest(srv, http.MethodGet, "/v1/events", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list apigen.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 events, got %d", len(list.Items))
	}
	for i := 1; i < len(list.Items); i++ {
		if list.Items[i-1].Ts.Before(list.Items[i].Ts) {
			t.Fatal("events must come back newest-first")
		}
	}
}

func TestListEventsFiltersByAction(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}

	rec := doRequest(srv, http.MethodGet, "/v1/events?action=exec.create", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list apigen.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("no exec was created, expected 0 events, got %d", len(list.Items))
	}
}

func TestListEventsRejectsAMalformedSince(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	if rec := doRequest(srv, http.MethodGet, "/v1/events?since=yesterday", token); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
