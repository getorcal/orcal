package snapshot

import "context"

type Repo interface {
	Create(ctx context.Context, s *Snapshot) error
	Get(ctx context.Context, id string) (*Snapshot, error)
	GetByName(ctx context.Context, name string) (*Snapshot, error)
	List(ctx context.Context, f Filter) ([]*Snapshot, error)
	CountChildren(ctx context.Context, id string) (int, error)
	Delete(ctx context.Context, id string) error
}
