package orcalclient_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/getorcal/orcal/internal/api"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
	"github.com/getorcal/orcal/internal/store/sqlite"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

func newClient(t *testing.T) (*orcalclient.Client, *fake.Fake, *exec.Service) {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f := fake.New()
	sandboxes := sandbox.NewService(st.Sandboxes(), f,
		sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}, "orcal")
	execs, err := exec.NewService(st.Execs(), sandboxes, f, filepath.Join(dir, "execs"), 1<<20)
	if err != nil {
		t.Fatalf("exec.NewService() error = %v", err)
	}
	snapshots := snapshot.NewService(st.Snapshots(), sandboxes, f)
	sandboxes.SetSnapshots(snapshots)
	fileSvc := files.NewService(sandboxes, f, files.Limits{
		FileMaxBytes:     1 << 20,
		ArchiveMaxBytes:  1 << 20,
		ListMaxEntries:   1000,
		ListMaxScanBytes: 1 << 20,
	})

	tokens := auth.NewService(auth.NewMemoryRepo())
	_, token, err := tokens.Create(context.Background(), auth.CreateOptions{Name: "client", Scopes: auth.Scopes{auth.ScopeAll}}, auth.Scopes{auth.ScopeAll})
	if err != nil {
		t.Fatalf("mint client token: %v", err)
	}

	srv := httptest.NewServer(api.NewServer(api.Options{
		Sandboxes: sandboxes,
		Execs:     execs,
		Snapshots: snapshots,
		Files:     fileSvc,
		Tokens:    tokens,
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	t.Cleanup(srv.Close)

	return orcalclient.New(srv.URL, token), f, execs
}

func TestClientCreatesAndReadsBackASandbox(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()

	created, err := c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine"})
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	if created.State != "running" {
		t.Errorf("state = %q, want running", created.State)
	}

	got, err := c.GetSandbox(ctx, "my-agent")
	if err != nil {
		t.Fatalf("GetSandbox() error = %v", err)
	}
	if got.Id != created.Id {
		t.Errorf("id = %s, want %s", got.Id, created.Id)
	}
}

func TestClientSurfacesAPIErrorsAsAPIError(t *testing.T) {
	c, _, _ := newClient(t)

	_, err := c.GetSandbox(context.Background(), "ghost")

	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *orcalclient.APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Code != "sandbox_not_found" {
		t.Errorf("Code = %q, want sandbox_not_found", apiErr.Code)
	}
	if apiErr.RequestID == "" {
		t.Error("RequestID is empty, want the value echoed by the server")
	}
}

func TestClientRejectsABadToken(t *testing.T) {
	c, _, _ := newClient(t)
	bad := orcalclient.New(c.BaseURL(), "wrong-token")

	_, err := bad.ListSandboxes(context.Background(), orcalclient.ListParams{})

	var apiErr *orcalclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("error = %v, want a 401 APIError", err)
	}
}

func TestClientStreamsOutputToCompletion(t *testing.T) {
	c, f, execs := newClient(t)
	ctx := context.Background()
	f.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("streamed")}},
	}, 5)
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine"})

	started, err := c.CreateExec(ctx, "my-agent", orcalclient.CreateExecParams{Command: []string{"echo"}})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}
	execs.Wait()

	var (
		collected []byte
		exitCode  *int
	)
	err = c.StreamOutput(ctx, started.Id, 0, func(e orcalclient.OutputEvent) error {
		switch e.Event {
		case "output":
			collected = append(collected, e.Data...)
		case "exit":
			exitCode = e.ExitCode
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if string(collected) != "streamed" {
		t.Errorf("collected = %q, want streamed", collected)
	}
	if exitCode == nil || *exitCode != 5 {
		t.Errorf("exit code = %v, want 5", exitCode)
	}
}

func TestClientStreamsFullMaxFramePayloadWithoutTruncation(t *testing.T) {
	c, f, execs := newClient(t)
	ctx := context.Background()
	payload := bytes.Repeat([]byte("x"), exec.MaxFramePayload)
	f.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: payload}},
	}, 0)
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine"})

	started, err := c.CreateExec(ctx, "my-agent", orcalclient.CreateExecParams{Command: []string{"echo"}})
	if err != nil {
		t.Fatalf("CreateExec() error = %v", err)
	}
	execs.Wait()

	var collected []byte
	err = c.StreamOutput(ctx, started.Id, 0, func(e orcalclient.OutputEvent) error {
		if e.Event == "output" {
			collected = append(collected, e.Data...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if len(collected) != exec.MaxFramePayload {
		t.Fatalf("collected length = %d, want %d", len(collected), exec.MaxFramePayload)
	}
	if !bytes.Equal(collected, payload) {
		t.Errorf("collected payload did not round-trip byte-for-byte")
	}
}

func TestClientDistinguishesGapEventsFromOutput(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/execs/exec-1/output", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: gap\ndata: {\"offset\":42}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: exit\ndata: {\"state\":\"exited\",\"exit_code\":0,\"truncated\":false}\n\n")
		flusher.Flush()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := orcalclient.New(srv.URL, "token")

	var events []orcalclient.OutputEvent
	err := c.StreamOutput(context.Background(), "exec-1", 0, func(e orcalclient.OutputEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Event != "gap" {
		t.Errorf("events[0].Event = %q, want gap", events[0].Event)
	}
	if events[0].Data != nil {
		t.Errorf("events[0].Data = %v, want nil", events[0].Data)
	}
	if events[0].Offset != 42 {
		t.Errorf("events[0].Offset = %d, want 42", events[0].Offset)
	}
}

func TestClientEscapesReservedCharactersInPathSegments(t *testing.T) {
	var gotPath, gotRawQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: exit\ndata: {\"state\":\"exited\",\"exit_code\":0,\"truncated\":false}\n\n")
		flusher.Flush()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := orcalclient.New(srv.URL, "token")

	id := "a?b#c/d"
	err := c.StreamOutput(context.Background(), id, 0, func(orcalclient.OutputEvent) error { return nil })
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}

	wantPath := "/v1/execs/" + id + "/output"
	if gotPath != wantPath {
		t.Errorf("server received path %q, want %q", gotPath, wantPath)
	}
	if gotRawQuery != "from=0" {
		t.Errorf("server received query %q, want %q", gotRawQuery, "from=0")
	}
}

func TestClientDoesNotCarryEventTypeAcrossBareDataLines(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/execs/exec-2/output", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: gap\ndata: {\"offset\":1}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"offset\":2}\n\n")
		flusher.Flush()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := orcalclient.New(srv.URL, "token")

	var events []orcalclient.OutputEvent
	err := c.StreamOutput(context.Background(), "exec-2", 0, func(e orcalclient.OutputEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamOutput() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Event != "gap" {
		t.Errorf("events[0].Event = %q, want gap", events[0].Event)
	}
	if events[1].Event != "" {
		t.Errorf("events[1].Event = %q, want empty (no preceding event: line)", events[1].Event)
	}
}

func TestClientListsAndDestroys(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	c.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "my-agent", Image: "alpine"})

	list, err := c.ListSandboxes(ctx, orcalclient.ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(list.Items))
	}

	destroyed, err := c.DestroySandbox(ctx, "my-agent")
	if err != nil {
		t.Fatalf("DestroySandbox() error = %v", err)
	}
	if destroyed.State != "destroyed" {
		t.Errorf("state = %q, want destroyed", destroyed.State)
	}
}
