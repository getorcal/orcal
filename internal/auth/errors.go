package auth

import "errors"

var (
	ErrNotFound        = errors.New("auth: token not found")
	ErrNameTaken       = errors.New("auth: token name is taken")
	ErrInvalidScope    = errors.New("auth: invalid scope")
	ErrScopeEscalation = errors.New("auth: requested scopes exceed the caller's own")
	ErrLastAdminToken  = errors.New("auth: refusing to revoke the last admin-capable token")
	ErrUnauthorized    = errors.New("auth: unauthorized")
	ErrInvalidName     = errors.New("auth: invalid token name")
)
