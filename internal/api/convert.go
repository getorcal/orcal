package api

import (
	"fmt"

	"github.com/getorcal/orcal/internal/apigen"
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
		ParentSnapshotId: s.ParentSnapshotID,
	}
	if s.Name != "" {
		out.Name = ptr(s.Name)
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
