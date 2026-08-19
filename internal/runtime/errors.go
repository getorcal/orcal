package runtime

import "errors"

var (
	ErrNotFound     = errors.New("runtime: not found")
	ErrConflict     = errors.New("runtime: conflict")
	ErrUnavailable  = errors.New("runtime: unavailable")
	ErrInvalidSpec  = errors.New("runtime: invalid spec")
	ErrPathNotFound = errors.New("runtime: path not found")
)
