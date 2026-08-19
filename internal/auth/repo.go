package auth

import (
	"context"
	"time"
)

type Repo interface {
	Create(ctx context.Context, t *Token) error
	Get(ctx context.Context, id string) (*Token, error)
	GetByHash(ctx context.Context, hash string) (*Token, error)
	GetByName(ctx context.Context, name string) (*Token, error)
	List(ctx context.Context) ([]*Token, error)
	Update(ctx context.Context, t *Token) error
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
}
