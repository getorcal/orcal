package audit

import (
	"context"
	"time"
)

type Repo interface {
	Create(ctx context.Context, e *Event) error
	List(ctx context.Context, f Filter) ([]*Event, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteBeyondCount(ctx context.Context, keep int) (int64, error)
}
