//go:build docker

package integration

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func TestSandboxContainerCarriesTheHardenedConfiguration(t *testing.T) {
	e := newEnv(t)
	id := e.sandbox(t, "integration-harden")
	ctx := context.Background()

	got, err := e.client.GetSandbox(ctx, id)
	if err != nil {
		t.Fatalf("GetSandbox() error = %v", err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	inspected, err := cli.ContainerInspect(ctx, containerIDFor(t, cli, got.Id))
	if err != nil {
		t.Fatalf("ContainerInspect() error = %v", err)
	}
	hc := inspected.HostConfig

	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", hc.CapDrop)
	}
	if len(hc.CapAdd) != 0 {
		t.Errorf("CapAdd = %v, want empty", hc.CapAdd)
	}
	foundNoNewPrivs := false
	for _, opt := range hc.SecurityOpt {
		if opt == "no-new-privileges:true" {
			foundNoNewPrivs = true
		}
	}
	if !foundNoNewPrivs {
		t.Errorf("SecurityOpt = %v, want no-new-privileges:true", hc.SecurityOpt)
	}
	if hc.Memory != 512<<20 {
		t.Errorf("Memory = %d, want %d", hc.Memory, int64(512)<<20)
	}
	if hc.MemorySwap != hc.Memory {
		t.Errorf("MemorySwap = %d, want %d so swap is disabled", hc.MemorySwap, hc.Memory)
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 128 {
		t.Errorf("PidsLimit = %v, want 128", hc.PidsLimit)
	}
	if len(hc.Binds) != 0 || len(hc.Mounts) != 0 {
		t.Errorf("Binds = %v, Mounts = %v, want both empty", hc.Binds, hc.Mounts)
	}
	if len(hc.PortBindings) != 0 {
		t.Errorf("PortBindings = %v, want empty", hc.PortBindings)
	}
	if hc.Privileged {
		t.Error("Privileged = true, want false")
	}
	if string(hc.NetworkMode) != testNetwork {
		t.Errorf("NetworkMode = %q, want %q", hc.NetworkMode, testNetwork)
	}
}

func TestCapabilitiesAreActuallyDroppedInsideTheSandbox(t *testing.T) {
	e := newEnv(t)
	e.sandbox(t, "integration-caps")

	output, code := e.runToCompletion(t, "integration-caps", "sh", "-c",
		"touch /tmp/orcal-chown-test && chown 1000:1000 /tmp/orcal-chown-test")

	if code == 0 {
		t.Errorf("chown succeeded inside the sandbox, want it to fail with CAP_CHOWN dropped; output = %q", output)
	}
}

func TestSandboxNetworkDisablesInterContainerCommunication(t *testing.T) {
	newEnv(t)
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	inspected, err := cli.NetworkInspect(ctx, testNetwork, network.InspectOptions{})
	if err != nil {
		t.Fatalf("NetworkInspect() error = %v", err)
	}
	if inspected.Options["com.docker.network.bridge.enable_icc"] != "false" {
		t.Errorf("enable_icc = %q, want false", inspected.Options["com.docker.network.bridge.enable_icc"])
	}
}
