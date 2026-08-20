package docker

import (
	"testing"

	"github.com/getorcal/orcal/internal/runtime"
)

func spec() runtime.CreateSpec {
	return runtime.CreateSpec{
		Image:       "alpine",
		CPUMillis:   2000,
		MemoryBytes: 4 << 30,
		PidsLimit:   256,
		NetworkName: "orcal",
	}
}

func TestHostConfigDropsAllCapabilities(t *testing.T) {
	hc := hostConfig(spec())

	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", hc.CapDrop)
	}
	if len(hc.CapAdd) != 0 {
		t.Errorf("CapAdd = %v, want empty", hc.CapAdd)
	}
}

func TestHostConfigSetsNoNewPrivileges(t *testing.T) {
	hc := hostConfig(spec())

	found := false
	for _, opt := range hc.SecurityOpt {
		if opt == "no-new-privileges:true" {
			found = true
		}
		if opt == "seccomp=unconfined" {
			t.Error("SecurityOpt disables seccomp, want the default profile retained")
		}
	}
	if !found {
		t.Errorf("SecurityOpt = %v, want no-new-privileges:true", hc.SecurityOpt)
	}
}

func TestHostConfigTranslatesResourceLimits(t *testing.T) {
	hc := hostConfig(spec())

	if hc.NanoCPUs != 2_000_000_000 {
		t.Errorf("NanoCPUs = %d, want 2000000000 for 2000 millis", hc.NanoCPUs)
	}
	if hc.Memory != 4<<30 {
		t.Errorf("Memory = %d, want %d", hc.Memory, int64(4)<<30)
	}
	if hc.MemorySwap != hc.Memory {
		t.Errorf("MemorySwap = %d, want %d so swap is disabled", hc.MemorySwap, hc.Memory)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 256 {
		t.Errorf("PidsLimit = %v, want 256", hc.PidsLimit)
	}
}

func TestHostConfigMountsNothingAndPublishesNothing(t *testing.T) {
	hc := hostConfig(spec())

	if len(hc.Binds) != 0 {
		t.Errorf("Binds = %v, want empty", hc.Binds)
	}
	if len(hc.Mounts) != 0 {
		t.Errorf("Mounts = %v, want empty", hc.Mounts)
	}
	if len(hc.PortBindings) != 0 {
		t.Errorf("PortBindings = %v, want empty", hc.PortBindings)
	}
	if hc.Privileged {
		t.Error("Privileged = true, want false")
	}
	if hc.PublishAllPorts {
		t.Error("PublishAllPorts = true, want false")
	}
}

func TestHostConfigAttachesToTheOrcalNetwork(t *testing.T) {
	hc := hostConfig(spec())

	if string(hc.NetworkMode) != "orcal" {
		t.Errorf("NetworkMode = %q, want orcal", hc.NetworkMode)
	}
}

func TestHostConfigCarriesTheResolvedOCIRuntime(t *testing.T) {
	s := spec()
	s.OCIRuntime = "runsc"
	hc := hostConfig(s)

	if hc.Runtime != "runsc" {
		t.Errorf("Runtime = %q, want runsc", hc.Runtime)
	}
}

func TestHostConfigLeavesRuntimeEmptyWhenUnset(t *testing.T) {
	hc := hostConfig(spec())

	if hc.Runtime != "" {
		t.Errorf("Runtime = %q, want empty so Docker uses its own default", hc.Runtime)
	}
}

func TestDiskQuotaSupportRequiresOverlay2OnXFS(t *testing.T) {
	cases := []struct {
		name   string
		driver string
		status [][2]string
		want   bool
	}{
		{"overlay2 on xfs", "overlay2", [][2]string{{"Backing Filesystem", "xfs"}}, true},
		{"overlay2 on ext4", "overlay2", [][2]string{{"Backing Filesystem", "extfs"}}, false},
		{"btrfs driver", "btrfs", [][2]string{{"Backing Filesystem", "btrfs"}}, false},
		{"no status reported", "overlay2", nil, false},
	}
	for _, c := range cases {
		if got := diskQuotaSupported(c.driver, c.status); got != c.want {
			t.Errorf("%s: diskQuotaSupported = %v, want %v", c.name, got, c.want)
		}
	}
}
