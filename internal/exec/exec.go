package exec

import "time"

type State string

const (
	StateRunning State = "running"
	StateExited  State = "exited"
	StateFailed  State = "failed"
)

type Exec struct {
	ID            string
	SandboxID     string
	RuntimeExecID string
	Command       []string
	Env           map[string]string
	WorkingDir    string
	User          string
	State         State
	ExitCode      *int
	OutputBytes   int64
	Truncated     bool
	StartedAt     time.Time
	FinishedAt    *time.Time
}

type Filter struct {
	Limit  int
	Cursor string
}
