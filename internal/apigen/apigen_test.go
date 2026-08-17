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
	if req.Image != "" {
		t.Errorf("zero CreateSandboxRequest.Image = %q, want empty", req.Image)
	}

	var list SandboxList
	if list.Items != nil {
		t.Error("zero SandboxList.Items is non-nil, want nil")
	}
}
