package docker

import (
	"context"
	"strings"
	"testing"
)

func TestResolveRuntimeUsesDaemonDefaultWhenUnset(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{Default: "runc", Available: []string{"runc", "runsc"}})
	got, err := d.ResolveRuntime(context.Background(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "runc" {
		t.Fatalf("an empty setting must report the daemon's own default, got %q", got)
	}
}

func TestResolveRuntimeAcceptsARegisteredRuntime(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{Default: "runc", Available: []string{"runc", "runsc"}})
	got, err := d.ResolveRuntime(context.Background(), "runsc")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "runsc" {
		t.Fatalf("expected runsc, got %q", got)
	}
}

func TestResolveRuntimeRefusesAnUnregisteredRuntime(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{Default: "runc", Available: []string{"runc"}})
	_, err := d.ResolveRuntime(context.Background(), "runsc")
	if err == nil {
		t.Fatal("a missing runtime must fail rather than silently fall back to the default")
	}
	if !strings.Contains(err.Error(), "runc") {
		t.Fatalf("the error must name the runtimes the daemon does have, got %v", err)
	}
}

func TestResolveRuntimeReportsANonRuncDefault(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{Default: "crun", Available: []string{"crun"}})
	got, err := d.ResolveRuntime(context.Background(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "crun" {
		t.Fatalf("the default must be read from the daemon, never hardcoded, got %q", got)
	}
}
