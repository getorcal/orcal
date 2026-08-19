package docker

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"

	"github.com/getorcal/orcal/internal/runtime"
)

func hostConfig(spec runtime.CreateSpec) *container.HostConfig {
	pids := int64(spec.PidsLimit)
	return &container.HostConfig{
		CapDrop:     strslice.StrSlice{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		NetworkMode: container.NetworkMode(spec.NetworkName),
		Privileged:  false,
		Resources: container.Resources{
			NanoCPUs:   int64(spec.CPUMillis) * 1_000_000,
			Memory:     spec.MemoryBytes,
			MemorySwap: spec.MemoryBytes,
			PidsLimit:  &pids,
		},
	}
}

func containerConfig(spec runtime.CreateSpec) *container.Config {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	labels := map[string]string{
		"orcal.managed": "true",
		"orcal.sandbox": spec.SandboxID,
	}
	for k, v := range spec.Labels {
		labels["orcal.label."+k] = v
	}
	return &container.Config{
		Image:     spec.Image,
		Env:       env,
		Labels:    labels,
		Tty:       false,
		OpenStdin: false,
		Cmd:       strslice.StrSlice{"sleep", "infinity"},
	}
}

func diskQuotaSupported(driver string, status [][2]string) bool {
	if driver != "overlay2" {
		return false
	}
	for _, pair := range status {
		if pair[0] == "Backing Filesystem" && pair[1] == "xfs" {
			return true
		}
	}
	return false
}
