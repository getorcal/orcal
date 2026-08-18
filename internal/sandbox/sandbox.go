package sandbox

import "time"

type Resources struct {
	CPUMillis   int
	MemoryBytes int64
	PidsLimit   int
}

type Sandbox struct {
	ID        string
	Name      string
	Image     string
	State     State
	Runtime   string
	RuntimeID string

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
