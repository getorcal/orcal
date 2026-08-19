package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/getorcal/orcal/internal/apigen"
)

func init() {
	openapi3filter.RegisterBodyDecoder("text/event-stream", openapi3filter.PlainBodyDecoder)
}

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

	snapResp, snapReq, snapBody := h.doCapturing(t, http.MethodPost, "/v1/sandboxes/my-agent/snapshots", map[string]any{"name": "v1"})
	if snapResp.StatusCode != http.StatusCreated {
		t.Fatalf("create snapshot status = %d, want 201", snapResp.StatusCode)
	}
	var createdSnapshot apigen.Snapshot
	if err := json.Unmarshal(snapBody, &createdSnapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	t.Run(snapReq.Method+" "+snapReq.URL.Path, func(t *testing.T) {
		assertMatchesContract(t, router, snapReq, snapResp, snapBody)
	})

	execResp, execReq, execBody := h.doCapturing(t, http.MethodPost, "/v1/sandboxes/my-agent/execs", map[string]any{
		"command": []string{"true"},
	})
	if execResp.StatusCode != http.StatusCreated {
		t.Fatalf("create exec status = %d, want 201", execResp.StatusCode)
	}
	var createdExec apigen.Exec
	if err := json.Unmarshal(execBody, &createdExec); err != nil {
		t.Fatalf("decode exec: %v", err)
	}
	h.execs.Wait()
	t.Run(execReq.Method+" "+execReq.URL.Path, func(t *testing.T) {
		assertMatchesContract(t, router, execReq, execResp, execBody)
	})

	runtimeID := h.fake.IDForSandbox(created.Id)
	h.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))

	putFileResp, putFileReq, putFileBody := h.doRawCapturing(t, http.MethodPut, "/v1/sandboxes/my-agent/files?path=/app/b.txt", "application/octet-stream", []byte("hello world"))
	if putFileResp.StatusCode != http.StatusNoContent {
		t.Fatalf("put file status = %d, want 204", putFileResp.StatusCode)
	}
	t.Run(putFileReq.Method+" "+putFileReq.URL.Path, func(t *testing.T) {
		assertMatchesContract(t, router, putFileReq, putFileResp, putFileBody)
	})

	archiveBody := buildTar(t, map[string][]byte{"c.txt": []byte("archived")})
	putArchiveResp, putArchiveReq, putArchiveBody := h.doRawCapturing(t, http.MethodPut, "/v1/sandboxes/my-agent/archive?path=/app", "application/x-tar", archiveBody)
	if putArchiveResp.StatusCode != http.StatusNoContent {
		t.Fatalf("put archive status = %d, want 204", putArchiveResp.StatusCode)
	}
	t.Run(putArchiveReq.Method+" "+putArchiveReq.URL.Path, func(t *testing.T) {
		assertMatchesContract(t, router, putArchiveReq, putArchiveResp, putArchiveBody)
	})

	seedAppFile := func(t *testing.T) {
		t.Helper()
		runtimeID := h.fake.IDForSandbox(created.Id)
		h.fake.Seed(runtimeID, "/app/a.txt", 0o644, []byte("hello"))
	}

	cases := []struct {
		method string
		path   string
		body   any
		setup  func(t *testing.T)
	}{
		{http.MethodGet, "/v1/healthz", nil, nil},
		{http.MethodGet, "/v1/version", nil, nil},
		{http.MethodGet, "/v1/sandboxes", nil, nil},
		{http.MethodGet, "/v1/sandboxes/my-agent", nil, nil},
		{http.MethodGet, "/v1/sandboxes/ghost", nil, nil},
		{http.MethodGet, "/v1/sandboxes/my-agent/execs", nil, nil},
		{http.MethodGet, "/v1/execs/" + createdExec.Id, nil, nil},
		{http.MethodGet, "/v1/execs/" + createdExec.Id + "/output", nil, nil},
		{http.MethodGet, "/v1/execs/" + createdExec.Id + "/output?from=notanumber", nil, nil},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"image": "alpine"}, nil},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"name": "my-agent", "image": "alpine"}, nil},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"name": "forked", "snapshot": "v1"}, nil},
		{http.MethodPost, "/v1/sandboxes", map[string]any{"name": "ghost-fork", "snapshot": "ghost-snapshot"}, nil},
		{http.MethodGet, "/v1/sandboxes/my-agent/snapshots", nil, nil},
		{http.MethodGet, "/v1/snapshots", nil, nil},
		{http.MethodGet, "/v1/snapshots?sandbox=ghost", nil, nil},
		{http.MethodGet, "/v1/snapshots/" + createdSnapshot.Id, nil, nil},
		{http.MethodPost, "/v1/sandboxes/my-agent/restore", map[string]any{"snapshot": "v1"}, nil},
		{http.MethodPost, "/v1/sandboxes/my-agent/start", nil, nil},
		{http.MethodPost, "/v1/sandboxes/my-agent/stop", nil, nil},
		{http.MethodGet, "/v1/sandboxes/my-agent/files?path=/app/a.txt", nil, seedAppFile},
		{http.MethodGet, "/v1/sandboxes/my-agent/files/stat?path=/app/a.txt", nil, seedAppFile},
		{http.MethodGet, "/v1/sandboxes/my-agent/files/list?path=/app", nil, seedAppFile},
		{http.MethodGet, "/v1/sandboxes/my-agent/archive?path=/app", nil, seedAppFile},
		{http.MethodDelete, "/v1/sandboxes/" + created.Id, nil, nil},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			if c.setup != nil {
				c.setup(t)
			}
			resp, req, body := h.doCapturing(t, c.method, c.path, c.body)
			assertMatchesContract(t, router, req, resp, body)
		})
	}

	createSandbox(t, h, "throwaway")
	toDelete := decode[apigen.Snapshot](t, h.do(t, http.MethodPost, "/v1/sandboxes/throwaway/snapshots", map[string]any{"name": "disposable"}))
	delResp, delReq, delBody := h.doCapturing(t, http.MethodDelete, "/v1/snapshots/"+toDelete.Id, nil)
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", delResp.StatusCode)
	}
	t.Run("DELETE /v1/snapshots/{ref}", func(t *testing.T) {
		assertMatchesContract(t, router, delReq, delResp, delBody)
	})
}
