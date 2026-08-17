package exec

import "context"

type Repo interface {
	Create(ctx context.Context, e *Exec) error
	Get(ctx context.Context, id string) (*Exec, error)
	ListBySandbox(ctx context.Context, sandboxID string, f Filter) ([]*Exec, error)
	ListRunning(ctx context.Context) ([]*Exec, error)
	Update(ctx context.Context, e *Exec) error
}
