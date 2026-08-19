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
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, found := strings.Cut(line, " ")
		if !found || pkg == allowed {
			continue
		}
		for imported := range strings.FieldsSeq(imports) {
			if strings.HasPrefix(imported, "github.com/docker/") {
				t.Errorf("%s imports %s; only %s may import Docker", pkg, imported, allowed)
			}
		}
	}
}

// The go directive is the minimum toolchain consumers need, and Go gates new runtime behavior
// on it — container-aware GOMAXPROCS is why this project is on 1.25 rather than the 1.23.0 its
// dependencies would allow. A build image older than the directive compiles fine and silently
// drops that behavior, so the two are asserted together.
func TestGoDirectiveMatchesTheBuildImage(t *testing.T) {
	mod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	const want = "go 1.25.0"
	directive := ""
	for line := range strings.SplitSeq(string(mod), "\n") {
		if strings.HasPrefix(line, "go ") {
			directive = line
			break
		}
	}
	if directive == "" {
		t.Fatal("go.mod has no go directive line")
	}
	if directive != want {
		t.Errorf("go.mod directive = %q, want %q; move it deliberately and never let `go mod tidy` move it for you", directive, want)
	}

	series := strings.TrimPrefix(strings.TrimSuffix(want, ".0"), "go ")
	dockerfile, err := os.ReadFile("../deploy/Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	if !strings.Contains(string(dockerfile), "FROM golang:"+series+"-alpine") {
		t.Errorf("deploy/Dockerfile does not build on golang:%s-alpine, so the shipped binary would not get the behavior the go directive enables", series)
	}
}
