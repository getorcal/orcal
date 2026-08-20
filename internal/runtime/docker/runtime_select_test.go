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
	d := newFakeDocker(t, fakeInfo{
		Default:   "runc",
		Available: []string{"runc", "runsc"},
		Args:      map[string][]string{"runsc": {runscOverlayArg, runscHostNetworkArg}},
	})
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

func TestResolveRuntimeAcceptsRunscWithBothRequiredArgs(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{
		Default:   "runc",
		Available: []string{"runc", "runsc"},
		Args:      map[string][]string{"runsc": {"--overlay2=none", "--network=host"}},
	})
	got, err := d.ResolveRuntime(context.Background(), "runsc")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "runsc" {
		t.Fatalf("expected runsc, got %q", got)
	}
}

func TestResolveRuntimeRefusesRunscMissingOverlayArg(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{
		Default:   "runc",
		Available: []string{"runc", "runsc"},
		Args:      map[string][]string{"runsc": {"--network=host"}},
	})
	_, err := d.ResolveRuntime(context.Background(), "runsc")
	if err == nil {
		t.Fatal("runsc without --overlay2=none must fail to start, not silently lose snapshot data")
	}
	if !strings.Contains(err.Error(), "--overlay2=none") {
		t.Fatalf("the error must name the missing flag, got %v", err)
	}
}

func TestResolveRuntimeRefusesRunscMissingNetworkArg(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{
		Default:   "runc",
		Available: []string{"runc", "runsc"},
		Args:      map[string][]string{"runsc": {"--overlay2=none"}},
	})
	_, err := d.ResolveRuntime(context.Background(), "runsc")
	if err == nil {
		t.Fatal("runsc without --network=host must fail to start, not silently boot with no route")
	}
	if !strings.Contains(err.Error(), "--network=host") {
		t.Fatalf("the error must name the missing flag, got %v", err)
	}
}

func TestResolveRuntimeAcceptsNonRunscRuntimeWithNoArgs(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{Default: "runc", Available: []string{"runc"}})
	got, err := d.ResolveRuntime(context.Background(), "runc")
	if err != nil {
		t.Fatalf("a runtime other than runsc has no runtimeArgs requirement, got error: %v", err)
	}
	if got != "runc" {
		t.Fatalf("expected runc, got %q", got)
	}
}

func TestResolveRuntimeAcceptsRunscWithExtraUnrelatedArgs(t *testing.T) {
	d := newFakeDocker(t, fakeInfo{
		Default:   "runc",
		Available: []string{"runc", "runsc"},
		Args:      map[string][]string{"runsc": {"--platform=amd64", "--overlay2=none", "--debug", "--network=host"}},
	})
	got, err := d.ResolveRuntime(context.Background(), "runsc")
	if err != nil {
		t.Fatalf("extra unrelated runtimeArgs alongside the required two must not fail resolution: %v", err)
	}
	if got != "runsc" {
		t.Fatalf("expected runsc, got %q", got)
	}
}
