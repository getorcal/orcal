package runtime

import (
	"context"
	"time"
)

type Stream int

const (
	StreamStdout Stream = iota + 1
	StreamStderr
)

type Frame struct {
	Stream Stream
	Data   []byte
}

type CreateSpec struct {
	SandboxID   string
	Image       string
	Env         map[string]string
	Labels      map[string]string
	CPUMillis   int
	MemoryBytes int64
	PidsLimit   int
	NetworkName string
}

type ExecSpec struct {
	Command    []string
	Env        map[string]string
	WorkingDir string
	User       string
}

type ContainerState string

const (
	ContainerRunning ContainerState = "running"
	ContainerStopped ContainerState = "stopped"
	ContainerGone    ContainerState = "gone"
)

type Status struct {
	State    ContainerState
	ExitCode *int
}

type ExecStatus struct {
	Running  bool
	ExitCode *int
}

type Session interface {
	ID() string
	Recv() (Frame, error)
	Wait(ctx context.Context) (int, error)
	Close() error
}

type Runtime interface {
	Create(ctx context.Context, spec CreateSpec) (string, error)
	Start(ctx context.Context, runtimeID string) error
	Stop(ctx context.Context, runtimeID string, timeout time.Duration) error
	Destroy(ctx context.Context, runtimeID string) error
	Inspect(ctx context.Context, runtimeID string) (Status, error)
	Exec(ctx context.Context, runtimeID string, spec ExecSpec) (Session, error)
	InspectExec(ctx context.Context, execRuntimeID string) (ExecStatus, error)
}
