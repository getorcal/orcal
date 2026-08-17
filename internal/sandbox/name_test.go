package sandbox

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNameAcceptsValidNames(t *testing.T) {
	valid := []string{"a", "my-agent", "agent1", "0", strings.Repeat("a", 63)}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
}

func TestValidateNameRejectsMalformedNames(t *testing.T) {
	invalid := []string{"", "-leading", "Upper", "under_score", "trailing-", strings.Repeat("a", 64), "has space"}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestValidateNameRejectsUUIDShapedNames(t *testing.T) {
	err := ValidateName("0192f3a4-5b6c-7d8e-9f01-23456789abcd")
	if err == nil {
		t.Fatal("ValidateName(uuid) = nil, want error")
	}
	if !errors.Is(err, ErrNameLooksLikeID) {
		t.Errorf("ValidateName(uuid) = %v, want ErrNameLooksLikeID", err)
	}
}
