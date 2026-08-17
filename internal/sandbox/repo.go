package sandbox

import "context"

type Repo interface {
	Create(ctx context.Context, s *Sandbox) error
	Get(ctx context.Context, id string) (*Sandbox, error)
	GetByName(ctx context.Context, name string) (*Sandbox, error)
	List(ctx context.Context, f Filter) ([]*Sandbox, error)
	Update(ctx context.Context, s *Sandbox) error
}
