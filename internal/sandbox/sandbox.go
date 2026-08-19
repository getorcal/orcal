package sandbox

import (
	"fmt"
	"time"
)

type Resources struct {
	CPUMillis   int
	MemoryBytes int64
	PidsLimit   int
}

type Network string

const (
	NetworkFull Network = "full"
	NetworkNone Network = "none"
)

func ValidateNetwork(n Network) error {
	switch n {
	case NetworkFull, NetworkNone:
		return nil
	default:
		return fmt.Errorf("%w: network must be full or none, got %q", ErrInvalidNetwork, n)
	}
}

type Sandbox struct {
	ID        string
	Name      string
	Image     string
	State     State
	Runtime   string
	RuntimeID string
	Network   Network

	ParentSnapshotID *string

	Resources Resources
	Env       map[string]string
	Labels    map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Filter struct {
	States []State
	Labels map[string]string
	Limit  int
	Cursor string
}
