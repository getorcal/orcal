package api

import (
	"log/slog"
	"net/http"

	"github.com/getorcal/orcal/internal/apigen"
	"github.com/getorcal/orcal/internal/auth"
	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/files"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

type Options struct {
	Sandboxes *sandbox.Service
	Execs     *exec.Service
	Snapshots *snapshot.Service
	Files     *files.Service
	TokenHash string
	Version   string
	Logger    *slog.Logger
}

type Server struct {
	sandboxes *sandbox.Service
	execs     *exec.Service
	snapshots *snapshot.Service
	files     *files.Service
	version   string
	logger    *slog.Logger
	handler   http.Handler
}

func NewServer(opts Options) *Server {
	s := &Server{
		sandboxes: opts.Sandboxes,
		execs:     opts.Execs,
		snapshots: opts.Snapshots,
		files:     opts.Files,
		version:   opts.Version,
		logger:    opts.Logger,
	}

	public := http.NewServeMux()
	private := http.NewServeMux()
	for _, r := range s.routes() {
		pattern := r.Method + " " + r.Path
		if r.Public {
			public.Handle(pattern, r.Handler)
			continue
		}
		private.Handle(pattern, r.Handler)
	}

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
