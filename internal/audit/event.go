package audit

import (
	"slices"
	"time"
)

type Action string

const (
	ActionSandboxCreate   Action = "sandbox.create"
	ActionSandboxFork     Action = "sandbox.fork"
	ActionSandboxStart    Action = "sandbox.start"
	ActionSandboxStop     Action = "sandbox.stop"
	ActionSandboxDestroy  Action = "sandbox.destroy"
	ActionSandboxRestore  Action = "sandbox.restore"
	ActionExecCreate      Action = "exec.create"
	ActionSnapshotCreate  Action = "snapshot.create"
	ActionSnapshotDelete  Action = "snapshot.delete"
	ActionFileRead        Action = "file.read"
	ActionFileWrite       Action = "file.write"
	ActionArchiveDownload Action = "archive.download"
	ActionArchiveUpload   Action = "archive.upload"
	ActionTokenCreate     Action = "token.create"
	ActionTokenRevoke     Action = "token.revoke"
	ActionAuthDenied      Action = "auth.denied"
)

var knownActions = []Action{
	ActionSandboxCreate, ActionSandboxFork, ActionSandboxStart, ActionSandboxStop,
	ActionSandboxDestroy, ActionSandboxRestore, ActionExecCreate, ActionSnapshotCreate,
	ActionSnapshotDelete, ActionFileRead, ActionFileWrite, ActionArchiveDownload,
	ActionArchiveUpload, ActionTokenCreate, ActionTokenRevoke, ActionAuthDenied,
}

func KnownActions() []Action {
	out := make([]Action, len(knownActions))
	copy(out, knownActions)
	return out
}

func ValidAction(a Action) bool {
	return slices.Contains(knownActions, a)
}

type Event struct {
	ID           string
	Timestamp    time.Time
	ActorTokenID string
	ActorName    string
	Action       Action
	ResourceType string
	ResourceID   string
	RequestID    string
	Status       int
	RemoteAddr   string
	Details      map[string]any
}

type Filter struct {
	Actor        string
	Action       Action
	ResourceType string
	ResourceID   string
	Since        time.Time
	Until        time.Time
	Limit        int
	Cursor       string
}
