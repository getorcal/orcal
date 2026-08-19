package internal_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// .Imports covers non-test files only; TestImports and XTestImports are deliberately excluded
// so build-tagged integration tests under test/integration may reach for the Docker client.
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

func TestGoDirectiveIsPinnedToTheDependencyFloor(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	const want = "go 1.23.0"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "go ") {
			if line != want {
				t.Errorf("go.mod directive = %q, want %q; this floor is held down by modernc.org/sqlite v1.39.0, github.com/getkin/kin-openapi v0.135.0, the oapi-codegen generate directive, and the golang:1.23-alpine build image — raising it means re-validating all four together, not just bumping this line", line, want)
			}
			return
		}
	}
	t.Fatal("go.mod has no go directive line")
}
