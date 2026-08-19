package apigen

import "testing"

func TestGeneratedTypesCoverTheContract(t *testing.T) {
	var s Sandbox
	if s.State != "" {
		t.Errorf("zero Sandbox.State = %q, want empty", s.State)
	}

	var e Exec
	if e.Truncated {
		t.Error("zero Exec.Truncated = true, want false")
	}

	var errBody ErrorBody
	if errBody.Code != "" {
		t.Errorf("zero ErrorBody.Code = %q, want empty", errBody.Code)
	}

	var req CreateSandboxRequest
	if req.Image != nil {
		t.Error("zero CreateSandboxRequest.Image is non-nil, want nil")
	}

	var list SandboxList
	if list.Items != nil {
		t.Error("zero SandboxList.Items is non-nil, want nil")
	}
}

func TestGeneratedSnapshotTypesCoverTheContract(t *testing.T) {
	var s Snapshot
	if s.Id != "" {
		t.Errorf("zero Snapshot.Id = %q, want empty", s.Id)
	}
	if s.ParentId != nil {
		t.Error("zero Snapshot.ParentId is non-nil, want nil")
	}

	var list SnapshotList
	if list.Items != nil {
		t.Error("zero SnapshotList.Items is non-nil, want nil")
	}

	var req RestoreRequest
	if req.Snapshot != "" {
		t.Errorf("zero RestoreRequest.Snapshot = %q, want empty", req.Snapshot)
	}

	var create CreateSandboxRequest
	if create.Snapshot != nil {
		t.Error("zero CreateSandboxRequest.Snapshot is non-nil, want nil")
	}

	var sb Sandbox
	if sb.ParentSnapshotId != nil {
		t.Error("zero Sandbox.ParentSnapshotId is non-nil, want nil")
	}
}

func TestGeneratedFileTypesCoverTheContract(t *testing.T) {
	var fi FileInfo
	if fi.Name != "" || fi.IsDir {
		t.Errorf("zero FileInfo = %+v, want empty", fi)
	}
	if fi.LinkTarget != nil {
		t.Error("zero FileInfo.LinkTarget is non-nil; it must be optional")
	}

	var fl FileList
	if fl.Items != nil {
		t.Error("zero FileList.Items is non-nil, want nil")
	}
	if fl.Truncated {
		t.Error("zero FileList.Truncated = true, want false")
	}
}
