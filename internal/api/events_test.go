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

func TestListEventsRejectsAMalformedUntil(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})
	if rec := doRequest(srv, http.MethodGet, "/v1/events?until=yesterday", token); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListEventsPaginatesNewestFirstWithoutOverlapOrGap(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	const total = 5
	for range total {
		if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
			t.Fatalf("create: %d", rec.Code)
		}
		time.Sleep(2 * time.Millisecond)
	}

	first := doRequest(srv, http.MethodGet, "/v1/events?limit=3", token)
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.Code, first.Body.String())
	}
	var page1 apigen.EventList
	if err := json.Unmarshal(first.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page1.Items) != 3 {
		t.Fatalf("expected a full page of 3, got %d", len(page1.Items))
	}
	if page1.NextCursor == nil {
		t.Fatal("a full page must carry a next_cursor")
	}
	oldestOnPage1 := page1.Items[len(page1.Items)-1].Id
	if *page1.NextCursor != oldestOnPage1 {
		t.Fatalf("next_cursor must be the oldest item on the page (last, under the descending sort), got %q want %q",
			*page1.NextCursor, oldestOnPage1)
	}

	second := doRequest(srv, http.MethodGet, "/v1/events?limit=3&cursor="+*page1.NextCursor, token)
	if second.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", second.Code, second.Body.String())
	}
	var page2 apigen.EventList
	if err := json.Unmarshal(second.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page2.Items) != total-3 {
		t.Fatalf("expected %d remaining events, got %d", total-3, len(page2.Items))
	}
	if page2.NextCursor != nil {
		t.Fatal("the final, short page must not carry a next_cursor")
	}
	for _, item := range page2.Items {
		if item.Id >= oldestOnPage1 {
			t.Fatalf("page 2 must only contain events older than the cursor, got %s >= %s", item.Id, oldestOnPage1)
		}
	}

	seen := map[string]bool{}
	for _, item := range append(append([]apigen.Event{}, page1.Items...), page2.Items...) {
		if seen[item.Id] {
			t.Fatalf("event %s appeared on both pages", item.Id)
		}
		seen[item.Id] = true
	}
	if len(seen) != total {
		t.Fatalf("expected %d distinct events across both pages combined, got %d", total, len(seen))
	}
}

func TestListEventsFiltersByActor(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	alice := mint(t, svc, "alice", auth.Scopes{auth.ScopeAll})
	bob := mint(t, svc, "bob", auth.Scopes{auth.ScopeAll})

	if rec := postJSON(srv, "/v1/sandboxes", alice, map[string]any{"image": "alpine", "name": "alice-box"}); rec.Code != http.StatusCreated {
		t.Fatalf("create alice sandbox: %d", rec.Code)
	}
	if rec := postJSON(srv, "/v1/sandboxes", bob, map[string]any{"image": "alpine", "name": "bob-box"}); rec.Code != http.StatusCreated {
		t.Fatalf("create bob sandbox: %d", rec.Code)
	}

	unfiltered := doRequest(srv, http.MethodGet, "/v1/events", alice)
	if unfiltered.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", unfiltered.Code)
	}
	var all apigen.EventList
	if err := json.Unmarshal(unfiltered.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var aliceID string
	for _, e := range all.Items {
		if e.ActorName != nil && *e.ActorName == "alice" {
			aliceID = *e.ActorTokenId
		}
	}
	if aliceID == "" {
		t.Fatal("could not find alice's actor token id in the unfiltered listing")
	}

	rec := doRequest(srv, http.MethodGet, "/v1/events?actor="+aliceID, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var filtered apigen.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(filtered.Items) != 1 {
		t.Fatalf("expected exactly alice's one event, got %d", len(filtered.Items))
	}
	if filtered.Items[0].ActorName == nil || *filtered.Items[0].ActorName != "alice" {
		t.Fatalf("expected alice's event, got %+v", filtered.Items[0])
	}
}

func TestListEventsFiltersByResourceID(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	first := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"})
	if first.Code != http.StatusCreated {
		t.Fatalf("create first: %d", first.Code)
	}
	var firstSandbox apigen.Sandbox
	if err := json.Unmarshal(first.Body.Bytes(), &firstSandbox); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
		t.Fatalf("create second: %d", rec.Code)
	}

	rec := doRequest(srv, http.MethodGet, "/v1/events?resource_id="+firstSandbox.Id, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list apigen.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected exactly one event for that resource, got %d", len(list.Items))
	}
	if list.Items[0].ResourceId == nil || *list.Items[0].ResourceId != firstSandbox.Id {
		t.Fatalf("expected the event for %s, got %+v", firstSandbox.Id, list.Items[0])
	}
}

func TestListEventsFiltersBySince(t *testing.T) {
	srv, svc, _ := newAuditTestServer(t)
	token := mint(t, svc, "root", auth.Scopes{auth.ScopeAll})

	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
		t.Fatalf("create first: %d", rec.Code)
	}
	time.Sleep(2 * time.Millisecond)
	cutoff := time.Now().UTC()
	time.Sleep(2 * time.Millisecond)
	if rec := postJSON(srv, "/v1/sandboxes", token, map[string]any{"image": "alpine"}); rec.Code != http.StatusCreated {
		t.Fatalf("create second: %d", rec.Code)
	}

	rec := doRequest(srv, http.MethodGet, "/v1/events?since="+cutoff.Format(time.RFC3339Nano), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list apigen.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected only the event after the cutoff, got %d", len(list.Items))
	}
	if !list.Items[0].Ts.After(cutoff) {
		t.Fatalf("returned event must be after the cutoff, got %s vs cutoff %s", list.Items[0].Ts, cutoff)
	}
}
