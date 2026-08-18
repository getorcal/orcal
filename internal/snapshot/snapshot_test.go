package snapshot

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNameAcceptsValidNames(t *testing.T) {
	for _, n := range []string{"a", "working-v1", "v17", "0", strings.Repeat("a", 63)} {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
}

func TestValidateNameRejectsMalformedNames(t *testing.T) {
	for _, n := range []string{"", "-leading", "Upper", "under_score", "trailing-", strings.Repeat("a", 64), "has space"} {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestValidateNameRejectsUUIDShapedNames(t *testing.T) {
	err := ValidateName("0192f3a4-5b6c-7d8e-9f01-23456789abcd")
	if !errors.Is(err, ErrNameLooksLikeID) {
		t.Errorf("ValidateName(uuid) = %v, want ErrNameLooksLikeID", err)
	}
}

func TestZeroSnapshotHasNilParent(t *testing.T) {
	var s Snapshot
	if s.ParentID != nil {
		t.Errorf("zero ParentID = %v, want nil", s.ParentID)
	}
}

func TestGeneratedIDsSortChronologically(t *testing.T) {
	ids := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		ids = append(ids, NewID())
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("ids not increasing at %d: %q >= %q", i, ids[i-1], ids[i])
		}
	}
}
