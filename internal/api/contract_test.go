package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/getorcal/orcal/internal/apigen"
)

func loadRouter(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile("../../spec/openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is not a valid document: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return doc, router
}

func assertMatchesContract(t *testing.T, router routers.Router, req *http.Request, resp *http.Response, body []byte) {
	t.Helper()
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("no contract route for %s %s: %v", req.Method, req.URL.Path, err)
	}
	err = openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		},
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   readCloser(body),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
		},
	})
	if err != nil {
		t.Errorf("%s %s -> %d violates the contract: %v", req.Method, req.URL.Path, resp.StatusCode, err)
	}
}

func TestResponsesMatchTheOpenAPIContract(t *testing.T) {
	_, router := loadRouter(t)
	h := newHarness(t)
	h.fake.SetExecScript(nil, 0)

	created := createSandbox(t, h, "my-agent")
	execResp := h.do(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"true"},
	})
	createdExec := decode[apigen.Exec](t, execResp)
	h.execs.Wait()

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/v1/healthz", nil},
		{http.MethodGet, "/v1/version", nil},
		{http.MethodGet, "/v1/sandboxes", nil},
		{http.MethodGet, "/v1/sandboxes/my-agent", nil},
		{http.MethodGet, "/v1/sandboxes/ghost", nil},
		{http.MethodGet, "/v1/sandboxes/my-agent/execs", nil},
		{http.MethodGet, "/v1/execs/" + createdExec.Id, nil},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"image": "alpine"}},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"name": "my-agent", "image": "alpine"}},
		{http.MethodPost, "/v1/sandboxes/my-agent/start", nil},
		{http.MethodPost, "/v1/sandboxes/my-agent/stop", nil},
		{http.MethodDelete, "/v1/sandboxes/" + created.Id, nil},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			resp, req, body := h.doCapturing(t, c.method, c.path, c.body)
			assertMatchesContract(t, router, req, resp, body)
		})
	}
}
