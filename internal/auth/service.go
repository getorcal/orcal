package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getorcal/orcal/internal/id"
)

const lastUsedInterval = time.Minute

type CreateOptions struct {
	Name      string
	Scopes    Scopes
	ExpiresIn time.Duration
}

type Service struct {
	repo  Repo
	now   func() time.Time
	newID func() string
}

func NewService(repo Repo) *Service {
	return &Service{
		repo:  repo,
		now:   func() time.Time { return time.Now().UTC() },
		newID: id.New,
	}
}

func (s *Service) Create(ctx context.Context, opts CreateOptions, grantor Scopes) (*Token, string, error) {
	if err := ValidateName(opts.Name); err != nil {
		return nil, "", err
	}
	if err := ValidateScopes(opts.Scopes); err != nil {
		return nil, "", err
	}
	if missing := grantor.Missing(opts.Scopes); len(missing) > 0 {
		return nil, "", fmt.Errorf("%w: %v", ErrScopeEscalation, missing)
	}
	if opts.ExpiresIn < 0 {
		return nil, "", fmt.Errorf("%w: expiry must not be negative", ErrInvalidName)
	}

	plaintext, err := GenerateToken()
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	tok := &Token{
		ID:        s.newID(),
		Name:      opts.Name,
		Hash:      HashToken(plaintext),
		Prefix:    PrefixOf(plaintext),
		Scopes:    opts.Scopes,
		CreatedAt: now,
	}
	if opts.ExpiresIn > 0 {
		expires := now.Add(opts.ExpiresIn)
		tok.ExpiresAt = &expires
	}
	if err := s.repo.Create(ctx, tok); err != nil {
		return nil, "", err
	}
	return tok, plaintext, nil
}

func (s *Service) List(ctx context.Context) ([]*Token, error) {
	return s.repo.List(ctx)
}

func (s *Service) Revoke(ctx context.Context, id string) error {
	tok, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if tok.RevokedAt != nil {
		return nil
	}

	now := s.now()
	if tok.Scopes.AdminCapable() {
		others, err := s.repo.List(ctx)
		if err != nil {
			return err
		}
		remaining := 0
		for _, other := range others {
			if other.ID != tok.ID && other.Live(now) && other.Scopes.AdminCapable() {
				remaining++
			}
		}
		if remaining == 0 {
			return fmt.Errorf("%w: restart orcald with ORCAL_TOKEN set to recover", ErrLastAdminToken)
		}
	}

	tok.RevokedAt = &now
	return s.repo.Update(ctx, tok)
}

func (s *Service) Authenticate(ctx context.Context, plaintext string) (*Token, error) {
	if plaintext == "" {
		return nil, ErrUnauthorized
	}
	tok, err := s.repo.GetByHash(ctx, HashToken(plaintext))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}

	now := s.now()
	if !tok.Live(now) {
		return nil, ErrUnauthorized
	}
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) >= lastUsedInterval {
		if err := s.repo.TouchLastUsed(ctx, tok.ID, now); err != nil {
			return nil, err
		}
	}
	return tok, nil
}
