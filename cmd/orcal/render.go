package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/pkg/orcalclient"
)

const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitNotFound = 3
	exitAuth     = 4
	exitSelf     = 125
)

var ErrConfirmationRequired = errors.New("orcal: confirmation required")

type selfFailure struct{ err error }

func (e selfFailure) Error() string { return e.err.Error() }

func (e selfFailure) Unwrap() error { return e.err }

func asSelfFailure(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *orcalclient.APIError
	if errors.As(err, &apiErr) {
		return err
	}
	var exit execExitError
	if errors.As(err, &exit) {
		return err
	}
	return selfFailure{err: err}
}

func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	if errors.Is(err, ErrConfirmationRequired) {
		return exitUsage
	}
	var apiErr *orcalclient.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "sandbox_not_found", "exec_not_found", "snapshot_not_found":
			return exitNotFound
		case "unauthorized":
			return exitAuth
		case "invalid_request":
			return exitUsage
		default:
			return exitError
		}
	}
	var self selfFailure
	if errors.As(err, &self) {
		return exitSelf
	}
	return exitError
}

func renderJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func renderJSONLine(w io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func printError(w io.Writer, jsonOutput bool, err error) error {
	if !jsonOutput {
		msg := err.Error()
		if !strings.HasPrefix(msg, "orcal:") {
			msg = "orcal: " + msg
		}
		_, writeErr := fmt.Fprintln(w, msg)
		return writeErr
	}
	payload := map[string]any{"message": err.Error()}
	var apiErr *orcalclient.APIError
	if errors.As(err, &apiErr) {
		payload["code"] = apiErr.Code
		payload["status_code"] = apiErr.StatusCode
	}
	return renderJSONLine(w, payload)
}

func renderSandbox(w io.Writer, format string, s *apigen.Sandbox) error {
	if format == "json" {
		return renderJSON(w, s)
	}
	fmt.Fprintf(w, "%s\n", s.Id)
	return nil
}

func renderSandboxInspect(w io.Writer, format string, s *apigen.Sandbox) error {
	if format == "json" {
		return renderJSON(w, s)
	}
	name := "-"
	if s.Name != nil {
		name = *s.Name
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "id:\t%s\n", s.Id)
	fmt.Fprintf(tw, "name:\t%s\n", name)
	fmt.Fprintf(tw, "image:\t%s\n", s.Image)
	fmt.Fprintf(tw, "state:\t%s\n", s.State)
	fmt.Fprintf(tw, "runtime:\t%s\n", s.Runtime)
	fmt.Fprintf(tw, "cpu_millis:\t%d\n", s.Resources.CpuMillis)
	fmt.Fprintf(tw, "memory_bytes:\t%d\n", s.Resources.MemoryBytes)
	fmt.Fprintf(tw, "pids_limit:\t%d\n", s.Resources.PidsLimit)
	fmt.Fprintf(tw, "created_at:\t%s\n", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "updated_at:\t%s\n", s.UpdatedAt.Format(time.RFC3339))
	return tw.Flush()
}

func renderSandboxList(w io.Writer, format string, list *apigen.SandboxList) error {
	if format == "json" {
		return renderJSON(w, list)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tIMAGE\tSTATE")
	for _, item := range list.Items {
		name := "-"
		if item.Name != nil {
			name = *item.Name
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.Id, name, item.Image, item.State)
	}
	return tw.Flush()
}

func renderExec(w io.Writer, format string, e *apigen.Exec) error {
	if format == "json" {
		return renderJSON(w, e)
	}
	fmt.Fprintf(w, "%s\n", e.Id)
	return nil
}
