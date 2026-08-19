package api

import (
	"net/http"

	"github.com/getorcal/orcal/internal/auth"
)

type route struct {
	Method  string
	Path    string
	Scope   auth.Scope
	Public  bool
	Action  string
	Audited bool
	Handler http.HandlerFunc
}

func (s *Server) routes() []route {
	return []route{
		{Method: "GET", Path: "/v1/healthz", Public: true, Handler: s.handleHealth},
		{Method: "GET", Path: "/v1/version", Public: true, Handler: s.handleVersion},

		{Method: "POST", Path: "/v1/sandboxes", Scope: auth.ScopeSandboxesWrite, Action: "sandbox.create", Audited: true, Handler: s.handleCreateSandbox},
		{Method: "GET", Path: "/v1/sandboxes", Scope: auth.ScopeSandboxesRead, Handler: s.handleListSandboxes},
		{Method: "GET", Path: "/v1/sandboxes/{ref}", Scope: auth.ScopeSandboxesRead, Handler: s.handleGetSandbox},
		{Method: "DELETE", Path: "/v1/sandboxes/{ref}", Scope: auth.ScopeSandboxesWrite, Action: "sandbox.destroy", Audited: true, Handler: s.handleDestroySandbox},
		{Method: "POST", Path: "/v1/sandboxes/{ref}/start", Scope: auth.ScopeSandboxesWrite, Action: "sandbox.start", Audited: true, Handler: s.handleStartSandbox},
		{Method: "POST", Path: "/v1/sandboxes/{ref}/stop", Scope: auth.ScopeSandboxesWrite, Action: "sandbox.stop", Audited: true, Handler: s.handleStopSandbox},

		{Method: "POST", Path: "/v1/sandboxes/{ref}/execs", Scope: auth.ScopeExec, Action: "exec.create", Audited: true, Handler: s.handleCreateExec},
		{Method: "GET", Path: "/v1/sandboxes/{ref}/execs", Scope: auth.ScopeExec, Handler: s.handleListExecs},
		{Method: "GET", Path: "/v1/execs/{id}", Scope: auth.ScopeExec, Handler: s.handleGetExec},
		{Method: "GET", Path: "/v1/execs/{id}/output", Scope: auth.ScopeExec, Handler: s.handleExecOutput},

		{Method: "POST", Path: "/v1/sandboxes/{ref}/snapshots", Scope: auth.ScopeSnapshotsWrite, Action: "snapshot.create", Audited: true, Handler: s.handleCreateSnapshot},
		{Method: "GET", Path: "/v1/sandboxes/{ref}/snapshots", Scope: auth.ScopeSnapshotsRead, Handler: s.handleListSandboxSnapshots},
		{Method: "POST", Path: "/v1/sandboxes/{ref}/restore", Scope: auth.ScopeSandboxesWrite, Action: "sandbox.restore", Audited: true, Handler: s.handleRestoreSandbox},
		{Method: "GET", Path: "/v1/snapshots", Scope: auth.ScopeSnapshotsRead, Handler: s.handleListSnapshots},
		{Method: "GET", Path: "/v1/snapshots/{ref}", Scope: auth.ScopeSnapshotsRead, Handler: s.handleGetSnapshot},
		{Method: "DELETE", Path: "/v1/snapshots/{ref}", Scope: auth.ScopeSnapshotsWrite, Action: "snapshot.delete", Audited: true, Handler: s.handleDeleteSnapshot},

		{Method: "GET", Path: "/v1/sandboxes/{ref}/files", Scope: auth.ScopeFilesRead, Action: "file.read", Audited: true, Handler: s.handleReadFile},
		{Method: "PUT", Path: "/v1/sandboxes/{ref}/files", Scope: auth.ScopeFilesWrite, Action: "file.write", Audited: true, Handler: s.handleWriteFile},
		{Method: "GET", Path: "/v1/sandboxes/{ref}/files/stat", Scope: auth.ScopeFilesRead, Handler: s.handleStatFile},
		{Method: "GET", Path: "/v1/sandboxes/{ref}/files/list", Scope: auth.ScopeFilesRead, Handler: s.handleListFiles},
		{Method: "GET", Path: "/v1/sandboxes/{ref}/archive", Scope: auth.ScopeFilesRead, Action: "archive.download", Audited: true, Handler: s.handleDownloadArchive},
		{Method: "PUT", Path: "/v1/sandboxes/{ref}/archive", Scope: auth.ScopeFilesWrite, Action: "archive.upload", Audited: true, Handler: s.handleUploadArchive},

		{Method: "POST", Path: "/v1/tokens", Scope: auth.ScopeAdmin, Action: "token.create", Audited: true, Handler: s.handleCreateToken},
		{Method: "GET", Path: "/v1/tokens", Scope: auth.ScopeAdmin, Handler: s.handleListTokens},
		{Method: "DELETE", Path: "/v1/tokens/{id}", Scope: auth.ScopeAdmin, Action: "token.revoke", Audited: true, Handler: s.handleRevokeToken},
	}
}
