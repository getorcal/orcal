package api_test

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
)

type outputEvent struct {
	name string
	data map[string]any
}

func readEvents(t *testing.T, resp *http.Response) []outputEvent {
	t.Helper()
	defer resp.Body.Close()

	var (
		events []outputEvent
		name   string
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("decode event data: %v", err)
			}
			events = append(events, outputEvent{name: name, data: payload})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output stream: %v", err)
	}
	return events
}

func TestCreateExecReturns201Running(t *testing.T) {
	h := newHarness(t)
	h.fake.SetExecScript(nil, 0)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"echo", "hi"},
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decode[apigen.Exec](t, resp)
	if got.State != "running" {
		t.Errorf("state = %q, want running", got.State)
	}
	if len(got.Command) != 2 || got.Command[0] != "echo" {
		t.Errorf("command = %v, want [echo hi]", got.Command)
	}
}

func TestCreateExecWithoutCommandReturns400(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{"command": []string{}})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
}

func TestCreateExecOnStoppedSandboxReturns409(t *testing.T) {
	h := newHarness(t)
	createSandbox(t, h, "my-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/stop", nil)

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"echo"},
	})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "invalid_state" {
		t.Errorf("code = %q, want invalid_state", body.Error.Code)
	}
}

func TestCreateExecOnUnknownSandboxReturns404(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, http.MethodPost, "/v1/sandboxes/ghost/execs", map[string]any{
		"command": []string{"echo"},
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetExecReportsExitCodeAfterCompletion(t *testing.T) {
	h := newHarness(t)
	h.fake.SetExecScript(nil, 7)
	createSandbox(t, h, "my-agent")
	created := decode[apigen.Exec](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"false"},
	}))
	h.execs.Wait()

	got := decode[apigen.Exec](t, h.do(t, http.MethodGet, "/v1/execs/"+created.Id, nil))

	if got.State != "exited" {
		t.Errorf("state = %q, want exited", got.State)
	}
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("exit_code = %v, want 7", got.ExitCode)
	}
}

func TestGetUnknownExecReturns404ExecNotFound(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, http.MethodGet, "/v1/execs/0192f3a4-5b6c-7d8e-9f01-23456789abcd", nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decode[apigen.Error](t, resp)
	if body.Error.Code != "exec_not_found" {
		t.Errorf("code = %q, want exec_not_found", body.Error.Code)
	}
}

func TestListExecsReturnsExecsForTheSandbox(t *testing.T) {
	h := newHarness(t)
	h.fake.SetExecScript(nil, 0)
	createSandbox(t, h, "my-agent")
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{"command": []string{"a"}})
	h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{"command": []string{"b"}})
	h.execs.Wait()

	list := decode[apigen.ExecList](t, h.do(t, http.MethodGet, "/v1/sandboxes/my-agent/execs", nil))

	if len(list.Items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(list.Items))
	}
}

func TestOutputStreamReplaysFramesThenExitEvent(t *testing.T) {
	h := newHarness(t)
	h.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("hello")}},
		{Frame: runtime.Frame{Stream: runtime.StreamStderr, Data: []byte("oops")}},
	}, 3)
	createSandbox(t, h, "my-agent")
	created := decode[apigen.Exec](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"echo"},
	}))
	h.execs.Wait()

	events := readEvents(t, h.do(t, http.MethodGet, "/v1/execs/"+created.Id+"/output", nil))

	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3 (two output, one exit)", len(events))
	}
	if events[0].name != "output" || events[0].data["stream"] != "stdout" {
		t.Errorf("events[0] = %+v, want stdout output", events[0])
	}
	decoded, err := base64.StdEncoding.DecodeString(events[0].data["data"].(string))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if string(decoded) != "hello" {
		t.Errorf("payload = %q, want hello", decoded)
	}
	if events[1].data["stream"] != "stderr" {
		t.Errorf("events[1].stream = %v, want stderr", events[1].data["stream"])
	}
	if events[2].name != "exit" {
		t.Fatalf("events[2].name = %q, want exit", events[2].name)
	}
	if events[2].data["exit_code"] != float64(3) {
		t.Errorf("exit_code = %v, want 3", events[2].data["exit_code"])
	}
}

func TestOutputStreamResumesFromOffset(t *testing.T) {
	h := newHarness(t)
	h.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("first")}},
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: []byte("second")}},
	}, 0)
	createSandbox(t, h, "my-agent")
	created := decode[apigen.Exec](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"echo"},
	}))
	h.execs.Wait()

	full := readEvents(t, h.do(t, http.MethodGet, "/v1/execs/"+created.Id+"/output", nil))
	firstOffset := int64(full[0].data["offset"].(float64))

	resumed := readEvents(t, h.do(t, http.MethodGet,
		"/v1/execs/"+created.Id+"/output?from="+itoa(firstOffset), nil))

	if len(resumed) != 2 {
		t.Fatalf("len(resumed) = %d, want 2 (second output, exit)", len(resumed))
	}
	decoded, _ := base64.StdEncoding.DecodeString(resumed[0].data["data"].(string))
	if string(decoded) != "second" {
		t.Errorf("resumed payload = %q, want second", decoded)
	}
}

func TestOutputStreamLargeFrameRoundTripsFullLength(t *testing.T) {
	h := newHarness(t)
	payload := bytes.Repeat([]byte("x"), exec.MaxFramePayload)
	h.fake.SetExecScript([]fake.Step{
		{Frame: runtime.Frame{Stream: runtime.StreamStdout, Data: payload}},
	}, 0)
	createSandbox(t, h, "my-agent")
	created := decode[apigen.Exec](t, h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"echo"},
	}))
	h.execs.Wait()

	events := readEvents(t, h.do(t, http.MethodGet, "/v1/execs/"+created.Id+"/output", nil))

	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (one output, one exit)", len(events))
	}
	decoded, err := base64.StdEncoding.DecodeString(events[0].data["data"].(string))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(decoded) != exec.MaxFramePayload {
		t.Fatalf("payload length = %d, want %d", len(decoded), exec.MaxFramePayload)
	}
	if !bytes.Equal(decoded, payload) {
		t.Errorf("payload did not round-trip byte-for-byte")
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
