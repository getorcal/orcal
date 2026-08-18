package orcalclient

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getorcal/orcal/internal/apigen"
)

type CreateExecParams struct {
	Command    []string          `json:"command"`
	Env        map[string]string `json:"env,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	User       string            `json:"user,omitempty"`
}

type OutputEvent struct {
	Event     string
	Stream    string
	Data      []byte
	Offset    int64
	ExitCode  *int
	Truncated bool
	State     string
}

func (c *Client) CreateExec(ctx context.Context, ref string, params CreateExecParams) (*apigen.Exec, error) {
	return do[apigen.Exec](c, ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(ref)+"/execs", params)
}

func (c *Client) GetExec(ctx context.Context, id string) (*apigen.Exec, error) {
	return do[apigen.Exec](c, ctx, http.MethodGet, "/v1/execs/"+url.PathEscape(id), nil)
}

func (c *Client) ListExecs(ctx context.Context, ref string, params ListParams) (*apigen.ExecList, error) {
	return do[apigen.ExecList](c, ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(ref)+"/execs?"+params.query().Encode(), nil)
}

func (c *Client) StreamOutput(ctx context.Context, id string, from int64, handler func(OutputEvent) error) error {
	path := "/v1/execs/" + url.PathEscape(id) + "/output?from=" + strconv.FormatInt(from, 10)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var event string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			parsed, err := parseEvent(event, strings.TrimPrefix(line, "data: "))
			if err != nil {
				return err
			}
			if err := handler(parsed); err != nil {
				return err
			}
			event = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("orcal: read output stream: %w", err)
	}
	return nil
}

func parseEvent(event, payload string) (OutputEvent, error) {
	var raw struct {
		Stream    string `json:"stream"`
		Data      string `json:"data"`
		Offset    int64  `json:"offset"`
		ExitCode  *int   `json:"exit_code"`
		Truncated bool   `json:"truncated"`
		State     string `json:"state"`
	}
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return OutputEvent{}, fmt.Errorf("orcal: decode output event: %w", err)
	}

	out := OutputEvent{
		Event:     event,
		Stream:    raw.Stream,
		Offset:    raw.Offset,
		ExitCode:  raw.ExitCode,
		Truncated: raw.Truncated,
		State:     raw.State,
	}
	if raw.Data != "" {
		decoded, err := base64.StdEncoding.DecodeString(raw.Data)
		if err != nil {
			return OutputEvent{}, fmt.Errorf("orcal: decode output payload: %w", err)
		}
		out.Data = decoded
	}
	return out, nil
}
