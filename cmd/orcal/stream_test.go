package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubResult struct {
	stdout string
	stderr string
	code   int
}

func runStubCLI(t *testing.T, baseURL string, args ...string) stubResult {
	t.Helper()
	done := make(chan stubResult, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		full := append([]string{"--url", baseURL, "--token", cliToken}, args...)
		code := execute(full, &stdout, &stderr)
		done <- stubResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
	}()
	select {
	case res := <-done:
		return res
	case <-time.After(10 * time.Second):
		t.Fatalf("orcal %v did not return within 10s", args)
		return stubResult{}
	}
}

func stubExecBody(id, state string, outputBytes int64) map[string]any {
	return map[string]any{
		"id":           id,
		"sandbox_id":   "sandbox-1",
		"command":      []string{"echo"},
		"state":        state,
		"output_bytes": outputBytes,
		"truncated":    false,
		"started_at":   time.Now().UTC().Format(time.RFC3339),
	}
}

func writeStubJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func openSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
}

func sendSSE(w http.ResponseWriter, event string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
	w.(http.Flusher).Flush()
}

func stdoutPayload(data string, offset int64) map[string]any {
	return map[string]any{
		"stream": "stdout",
		"data":   base64.StdEncoding.EncodeToString([]byte(data)),
		"offset": offset,
	}
}

func exitPayload(code int) map[string]any {
	return map[string]any{"state": "exited", "exit_code": code, "truncated": false}
}

func TestCLIExecExitsSelfWhenTheStreamEndsWithoutAnExitEvent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sandboxes/{ref}/execs", func(w http.ResponseWriter, r *http.Request) {
		writeStubJSON(w, http.StatusCreated, stubExecBody("exec-1", "running", 0))
	})
	mux.HandleFunc("GET /v1/execs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		openSSE(w)
		sendSSE(w, "output", stdoutPayload("half a line", 16))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := runStubCLI(t, srv.URL, "exec", "my-agent", "--", "echo", "hi")

	if res.code != exitSelf {
		t.Errorf("exit = %d, want %d when the stream ends without an exit event", res.code, exitSelf)
	}
	if !strings.Contains(res.stdout, "half a line") {
		t.Errorf("stdout = %q, want the output delivered before the stream ended", res.stdout)
	}
	if !strings.Contains(res.stderr, "exit event") {
		t.Errorf("stderr = %q, want a diagnostic naming the missing exit event", res.stderr)
	}
}

func TestCLIExecExitsSelfWhenTheDaemonIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachable := srv.URL
	srv.Close()

	res := runStubCLI(t, unreachable, "exec", "my-agent", "--", "echo", "hi")

	if res.code != exitSelf {
		t.Errorf("exit = %d, want %d for a connection failure during exec", res.code, exitSelf)
	}
	if !strings.Contains(res.stderr, "orcal:") {
		t.Errorf("stderr = %q, want an orcal-level diagnostic", res.stderr)
	}
}

func TestCLILogsWithoutFollowReturnsAtTheCurrentOffset(t *testing.T) {
	const available = 16
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/execs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeStubJSON(w, http.StatusOK, stubExecBody("exec-1", "running", available))
	})
	mux.HandleFunc("GET /v1/execs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		openSSE(w)
		sendSSE(w, "output", stdoutPayload("so far", available))
		sendSSE(w, "output", stdoutPayload("written later", 32))
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := runStubCLI(t, srv.URL, "logs", "exec-1")

	if res.code != 0 {
		t.Fatalf("exit = %d, want 0", res.code)
	}
	if !strings.Contains(res.stdout, "so far") {
		t.Errorf("stdout = %q, want the output available at the current offset", res.stdout)
	}
	if strings.Contains(res.stdout, "written later") {
		t.Errorf("stdout = %q, want logs without --follow to stop at the current offset", res.stdout)
	}
}

func TestCLILogsWithFollowStreamsUntilTheExecFinishes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/execs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeStubJSON(w, http.StatusOK, stubExecBody("exec-1", "running", 16))
	})
	mux.HandleFunc("GET /v1/execs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		openSSE(w)
		sendSSE(w, "output", stdoutPayload("first ", 16))
		sendSSE(w, "output", stdoutPayload("second", 32))
		sendSSE(w, "exit", exitPayload(0))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := runStubCLI(t, srv.URL, "logs", "--follow", "exec-1")

	if res.code != 0 {
		t.Fatalf("exit = %d, want 0", res.code)
	}
	if res.stdout != "first second" {
		t.Errorf("stdout = %q, want the whole stream up to the exit event", res.stdout)
	}
}

func TestCLILogsFollowReconnectsFromTheLastOffsetWithoutDuplicating(t *testing.T) {
	var (
		mu    sync.Mutex
		froms []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/execs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeStubJSON(w, http.StatusOK, stubExecBody("exec-1", "running", 0))
	})
	mux.HandleFunc("GET /v1/execs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt := len(froms)
		froms = append(froms, r.URL.Query().Get("from"))
		mu.Unlock()

		openSSE(w)
		if attempt == 0 {
			sendSSE(w, "output", stdoutPayload("first half ", 16))
			return
		}
		sendSSE(w, "output", stdoutPayload("second half", 32))
		sendSSE(w, "exit", exitPayload(0))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := runStubCLI(t, srv.URL, "logs", "--follow", "exec-1")

	if res.code != 0 {
		t.Fatalf("exit = %d, want 0", res.code)
	}
	if res.stdout != "first half second half" {
		t.Errorf("stdout = %q, want both halves stitched with nothing duplicated", res.stdout)
	}
	if !strings.Contains(res.stderr, "reconnect") {
		t.Errorf("stderr = %q, want a reconnect notice", res.stderr)
	}
	mu.Lock()
	got := append([]string(nil), froms...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("output requests = %v, want the first stream plus one reconnect", got)
	}
	if got[0] != "0" || got[1] != "16" {
		t.Errorf("from parameters = %v, want [0 16] so the reconnect resumes at the last offset", got)
	}
}

func TestCLILogsFollowStopsReconnectingAfterABoundedNumberOfAttempts(t *testing.T) {
	var (
		mu       sync.Mutex
		attempts int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/execs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeStubJSON(w, http.StatusOK, stubExecBody("exec-1", "running", 0))
	})
	mux.HandleFunc("GET /v1/execs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		openSSE(w)
		sendSSE(w, "output", stdoutPayload("x", 16))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := runStubCLI(t, srv.URL, "logs", "--follow", "exec-1")

	if res.code != 0 {
		t.Fatalf("exit = %d, want 0 - logs reports the read, not the command", res.code)
	}
	mu.Lock()
	got := attempts
	mu.Unlock()
	if got < 2 {
		t.Errorf("output requests = %d, want at least one reconnect", got)
	}
	if got > 5 {
		t.Errorf("output requests = %d, want a bounded number of reconnects", got)
	}
}
