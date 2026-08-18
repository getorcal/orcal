package api

import (
	"log/slog"
	"net/http"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

type Options struct {
	Sandboxes *sandbox.Service
	Execs     *exec.Service
	Snapshots *snapshot.Service
	TokenHash string
	Version   string
	Logger    *slog.Logger
}

type Server struct {
	sandboxes *sandbox.Service
	execs     *exec.Service
	snapshots *snapshot.Service
	version   string
	logger    *slog.Logger
	handler   http.Handler
}

func NewServer(opts Options) *Server {
	s := &Server{
		sandboxes: opts.Sandboxes,
		execs:     opts.Execs,
		snapshots: opts.Snapshots,
		version:   opts.Version,
		logger:    opts.Logger,
	}

	public := http.NewServeMux()
	public.HandleFunc("GET /v1/healthz", s.handleHealth)
	public.HandleFunc("GET /v1/version", s.handleVersion)

	private := http.NewServeMux()
	private.HandleFunc("POST /v1/sandboxes", s.handleCreateSandbox)
	private.HandleFunc("GET /v1/sandboxes", s.handleListSandboxes)
	private.HandleFunc("GET /v1/sandboxes/{ref}", s.handleGetSandbox)
	private.HandleFunc("DELETE /v1/sandboxes/{ref}", s.handleDestroySandbox)
	private.HandleFunc("POST /v1/sandboxes/{ref}/start", s.handleStartSandbox)
	private.HandleFunc("POST /v1/sandboxes/{ref}/stop", s.handleStopSandbox)
	s.registerExecRoutes(private)
	s.registerSnapshotRoutes(private)

	root := http.NewServeMux()
	root.Handle("/v1/healthz", public)
	root.Handle("/v1/version", public)
	root.Handle("/", auth.Middleware(opts.TokenHash)(private))

	s.handler = requestIDMiddleware(recoveryMiddleware(s.logger)(loggingMiddleware(s.logger)(root)))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apigen.Version{Version: s.version, ApiVersions: []string{"v1"}})
}
