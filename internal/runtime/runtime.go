package runtime

import (
	"context"
	"io"
	"io/fs"
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
	OCIRuntime  string
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
	ContainerPaused  ContainerState = "paused"
	ContainerStopped ContainerState = "stopped"
	ContainerGone    ContainerState = "gone"
)

type SnapshotInfo struct {
	Ref       string
	SizeBytes int64
}

type Status struct {
	State    ContainerState
	ExitCode *int
}

type ExecStatus struct {
	Running  bool
	ExitCode *int
}

type FileInfo struct {
	Name       string
	LinkTarget string
	Size       int64
	Mode       fs.FileMode
	ModTime    time.Time
	IsDir      bool
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
	Snapshot(ctx context.Context, runtimeID string) (SnapshotInfo, error)
	DeleteSnapshot(ctx context.Context, ref string) error
	Unpause(ctx context.Context, runtimeID string) error
	StatPath(ctx context.Context, runtimeID, path string) (FileInfo, error)
	ReadArchive(ctx context.Context, runtimeID, path string) (io.ReadCloser, error)
	WriteArchive(ctx context.Context, runtimeID, destDir string, tar io.Reader) error
}
