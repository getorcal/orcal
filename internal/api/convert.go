package api

import (
	"fmt"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/audit"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

func ptr[T any](v T) *T { return &v }

func toAPISandbox(s *sandbox.Sandbox) apigen.Sandbox {
	out := apigen.Sandbox{
		Id:        s.ID,
		Image:     s.Image,
		State:     apigen.SandboxState(s.State),
		Runtime:   s.Runtime,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Resources: apigen.Resources{
			CpuMillis:   s.Resources.CPUMillis,
			MemoryBytes: s.Resources.MemoryBytes,
			PidsLimit:   s.Resources.PidsLimit,
		},
		Network:          apigen.Network(s.Network),
		ParentSnapshotId: s.ParentSnapshotID,
	}
	if s.Name != "" {
		out.Name = ptr(s.Name)
	}
	if s.OCIRuntime != "" {
		out.OciRuntime = ptr(s.OCIRuntime)
	}
	if len(s.Env) > 0 {
		out.Env = ptr(s.Env)
	}
	if len(s.Labels) > 0 {
		out.Labels = ptr(s.Labels)
	}
	return out
}

func toAPISnapshot(s *snapshot.Snapshot) apigen.Snapshot {
	out := apigen.Snapshot{
		Id:         s.ID,
		SandboxId:  s.SandboxID,
		ParentId:   s.ParentID,
		RuntimeRef: s.RuntimeRef,
		Image:      s.Image,
		SizeBytes:  s.SizeBytes,
		CreatedAt:  s.CreatedAt,
	}
	if s.Name != "" {
		out.Name = ptr(s.Name)
	}
	if s.Network != "" {
		out.Network = ptr(apigen.Network(s.Network))
	}
	return out
}

func toAPIFileInfo(info runtime.FileInfo) apigen.FileInfo {
	out := apigen.FileInfo{
		Name:  info.Name,
		Size:  info.Size,
		Mode:  fmt.Sprintf("%04o", info.Mode.Perm()),
		Mtime: info.ModTime,
		IsDir: info.IsDir,
	}
	if info.LinkTarget != "" {
		out.LinkTarget = ptr(info.LinkTarget)
	}
	return out
}

func apiScopes(scopes auth.Scopes) []apigen.Scope {
	out := make([]apigen.Scope, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, apigen.Scope(scope))
	}
	return out
}

func toAPIToken(t *auth.Token) apigen.Token {
	return apigen.Token{
		Id:         t.ID,
		Name:       t.Name,
		Prefix:     t.Prefix,
		Scopes:     apiScopes(t.Scopes),
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
	}
}

func toAPIEvent(e *audit.Event) apigen.Event {
	out := apigen.Event{
		Id:        e.ID,
		Ts:        e.Timestamp,
		Action:    string(e.Action),
		Status:    e.Status,
		RequestId: e.RequestID,
	}
	if e.ActorTokenID != "" {
		out.ActorTokenId = ptr(e.ActorTokenID)
	}
	if e.ActorName != "" {
		out.ActorName = ptr(e.ActorName)
	}
	if e.ResourceType != "" {
		out.ResourceType = ptr(e.ResourceType)
	}
	if e.ResourceID != "" {
		out.ResourceId = ptr(e.ResourceID)
	}
	if e.RemoteAddr != "" {
		out.RemoteAddr = ptr(e.RemoteAddr)
	}
	if len(e.Details) > 0 {
		out.Details = ptr(e.Details)
	}
	return out
}

func toAPIExec(e *exec.Exec) apigen.Exec {
	out := apigen.Exec{
		Id:          e.ID,
		SandboxId:   e.SandboxID,
		Command:     e.Command,
		State:       apigen.ExecState(e.State),
		OutputBytes: e.OutputBytes,
		Truncated:   e.Truncated,
		StartedAt:   e.StartedAt,
		ExitCode:    e.ExitCode,
		FinishedAt:  e.FinishedAt,
	}
	if len(e.Env) > 0 {
		out.Env = ptr(e.Env)
	}
	if e.WorkingDir != "" {
		out.WorkingDir = ptr(e.WorkingDir)
	}
	if e.User != "" {
		out.User = ptr(e.User)
	}
	return out
}
