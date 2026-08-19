package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type openAPIDoc struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true,
	"patch": true, "head": true, "options": true,
}

func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("parsed zero paths from openapi.yaml; the parser is not reading the document")
	}

	out := map[string]bool{}
	for path, operations := range doc.Paths {
		for method := range operations {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}
			out[strings.ToUpper(method)+" /v1"+path] = true
		}
	}
	return out
}

func routeKeys(t *testing.T) map[string]route {
	t.Helper()
	out := map[string]route{}
	for _, r := range (&Server{}).routes() {
		key := r.Method + " " + r.Path
		if _, duplicate := out[key]; duplicate {
			t.Fatalf("route %s is registered twice", key)
		}
		out[key] = r
	}
	return out
}

func TestEveryOpenAPIRouteIsGuarded(t *testing.T) {
	spec := specOperations(t)
	registry := routeKeys(t)

	for key := range spec {
		if _, ok := registry[key]; !ok {
			t.Errorf("%s is declared in openapi.yaml but has no registry entry, so it ships unguarded", key)
		}
	}
	for key := range registry {
		if !spec[key] {
			t.Errorf("%s is in the registry but not in openapi.yaml", key)
		}
	}
}

func TestEveryRouteIsEitherPublicOrScoped(t *testing.T) {
	for key, r := range routeKeys(t) {
		switch {
		case r.Public && r.Scope != "":
			t.Errorf("%s is marked public and also carries scope %q", key, r.Scope)
		case !r.Public && r.Scope == "":
			t.Errorf("%s is neither public nor scoped", key)
		}
	}
}

func TestOnlyHealthAndVersionArePublic(t *testing.T) {
	var public []string
	for key, r := range routeKeys(t) {
		if r.Public {
			public = append(public, key)
		}
	}
	if len(public) != 2 {
		t.Fatalf("expected exactly two public routes, got %v", public)
	}
	for _, key := range public {
		if key != "GET /v1/healthz" && key != "GET /v1/version" {
			t.Errorf("%s must not be public", key)
		}
	}
}

func TestEveryAuditedRouteNamesAnAction(t *testing.T) {
	for key, r := range routeKeys(t) {
		if r.Audited && r.Action == "" {
			t.Errorf("%s is audited but names no action", key)
		}
		if !r.Audited && r.Action != "" {
			t.Errorf("%s names action %q but is not audited", key, r.Action)
		}
	}
}

func TestEveryMutationIsAudited(t *testing.T) {
	for key, r := range routeKeys(t) {
		if r.Method == http.MethodGet || r.Public {
			continue
		}
		if !r.Audited {
			t.Errorf("%s mutates state but is not audited", key)
		}
	}
}
