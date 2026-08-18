package internal_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOnlyTheDockerAdapterImportsDocker(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "../...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	const allowed = "github.com/getorcal/orcal/internal/runtime/docker"
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, found := strings.Cut(line, " ")
		if !found || pkg == allowed {
			continue
		}
		for _, imported := range strings.Fields(imports) {
			if strings.HasPrefix(imported, "github.com/docker/") {
				t.Errorf("%s imports %s; only %s may import Docker", pkg, imported, allowed)
			}
		}
	}
}
