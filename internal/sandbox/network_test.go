package sandbox

import (
	"errors"
	"testing"
)

func TestValidateNetwork(t *testing.T) {
	for _, ok := range []Network{NetworkFull, NetworkNone} {
		if err := ValidateNetwork(ok); err != nil {
			t.Fatalf("%q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []Network{"", "None", "internal", "host", "bridge"} {
		if err := ValidateNetwork(bad); !errors.Is(err, ErrInvalidNetwork) {
			t.Fatalf("%q must be rejected, got %v", bad, err)
		}
	}
}

func TestNetworkConstants(t *testing.T) {
	if NetworkFull != "full" {
		t.Fatalf("the wire value must be full, got %q", NetworkFull)
	}
	if NetworkNone != "none" {
		t.Fatalf("the wire value must be none, got %q", NetworkNone)
	}
}
